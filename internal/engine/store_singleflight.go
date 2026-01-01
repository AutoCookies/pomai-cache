package engine

import (
	"context"
	"time"
)

// GetOrLoad gộp các request concurrent cùng key lại thành 1 (Request Coalescing).
// Kỹ thuật này giúp chống "Thundering Herd" (Đám đông giẫm đạp) khi Cache Miss.
//
// Cơ chế:
// 1. Nếu 100 request cùng hỏi key "A" và cache miss.
// 2. Chỉ 1 request được phép chạy hàm loader (gọi DB/API).
// 3. 99 request còn lại chờ.
// 4. Khi loader xong, kết quả trả về cho cả 100 request cùng lúc.
func (s *Store) GetOrLoad(ctx context.Context, key string, loader func(context.Context) ([]byte, time.Duration, error)) ([]byte, bool, error) {
	if key == "" {
		return nil, false, nil
	}

	// 1. Fast path: Kiểm tra cache trước
	if v, ok := s.Get(key); ok {
		return v, true, nil
	}

	// 2. Dùng Singleflight để gộp request
	// Sử dụng DoChan để hỗ trợ Context Cancellation (nếu client hủy request thì ta hủy theo)
	resCh := s.g.DoChan(key, func() (interface{}, error) {
		// Gọi loader (Hàm lấy dữ liệu từ nguồn gốc - DB/API)
		v, ttl, err := loader(ctx)
		if err != nil {
			return nil, err
		}

		// Best-effort: Lưu vào cache.
		// Bỏ qua lỗi (ví dụ đầy bộ nhớ) để ưu tiên trả dữ liệu cho user.
		_ = s.Put(key, v, ttl)

		return v, nil
	})

	select {
	case r := <-resCh:
		if r.Err != nil {
			return nil, false, r.Err
		}
		// Ép kiểu dữ liệu trả về
		if data, ok := r.Val.([]byte); ok {
			return data, true, nil
		}
		return nil, false, nil

	case <-ctx.Done():
		// Nếu context timeout hoặc cancel
		return nil, false, ctx.Err()
	}
}
