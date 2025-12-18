package httpadapter

import (
	"net/http"
)

// CorsMiddleware cấu hình cho phép tất cả các nguồn (Allow All)
// Phù hợp cho giai đoạn phát triển hoặc khi có nhiều frontend liên kết.
func CorsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Cho phép bất kỳ Origin nào gửi request tới
		// Lưu ý: Khi Allow-Origin là "*", không được phép dùng Allow-Credentials: true.
		// Nhưng vì bạn dùng Cookie (withCredentials), ta sẽ lấy chính Origin từ request.
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE, HEAD")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept, X-Requested-With")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		// Xử lý yêu cầu Preflight (OPTIONS)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
