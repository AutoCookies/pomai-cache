// File: packages/ds/vector/distance_simd.go
package vector

import (
	"math"
)

func CosineSIMD(a, b []float32) float32 {
	if len(a) != len(b) {
		return float32(math.MaxFloat32)
	}

	dim := len(a)
	var dotProduct float32
	var normA float32
	var normB float32

	i := 0
	for ; i+4 <= dim; i += 4 {
		dotProduct += a[i]*b[i] + a[i+1]*b[i+1] + a[i+2]*b[i+2] + a[i+3]*b[i+3]
		normA += a[i]*a[i] + a[i+1]*a[i+1] + a[i+2]*a[i+2] + a[i+3]*a[i+3]
		normB += b[i]*b[i] + b[i+1]*b[i+1] + b[i+2]*b[i+2] + b[i+3]*b[i+3]
	}

	for ; i < dim; i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 1.0
	}

	normASqrt := float32(math.Sqrt(float64(normA)))
	normBSqrt := float32(math.Sqrt(float64(normB)))
	similarity := dotProduct / (normASqrt * normBSqrt)

	return 1.0 - similarity
}

func EuclideanSIMD(a, b []float32) float32 {
	if len(a) != len(b) {
		return float32(math.MaxFloat32)
	}

	dim := len(a)
	var sum float32

	i := 0
	for ; i+4 <= dim; i += 4 {
		d0 := a[i] - b[i]
		d1 := a[i+1] - b[i+1]
		d2 := a[i+2] - b[i+2]
		d3 := a[i+3] - b[i+3]
		sum += d0*d0 + d1*d1 + d2*d2 + d3*d3
	}

	for ; i < dim; i++ {
		diff := a[i] - b[i]
		sum += diff * diff
	}

	return float32(math.Sqrt(float64(sum)))
}

func DotProductSIMD(a, b []float32) float32 {
	if len(a) != len(b) {
		return float32(math.MinInt32)
	}

	dim := len(a)
	var sum float32

	i := 0
	for ; i+4 <= dim; i += 4 {
		sum += a[i]*b[i] + a[i+1]*b[i+1] + a[i+2]*b[i+2] + a[i+3]*b[i+3]
	}

	for ; i < dim; i++ {
		sum += a[i] * b[i]
	}

	return -sum
}

func ManhattanSIMD(a, b []float32) float32 {
	if len(a) != len(b) {
		return float32(math.MaxFloat32)
	}

	dim := len(a)
	var sum float32

	i := 0
	for ; i+4 <= dim; i += 4 {
		d0 := a[i] - b[i]
		if d0 < 0 {
			d0 = -d0
		}
		d1 := a[i+1] - b[i+1]
		if d1 < 0 {
			d1 = -d1
		}
		d2 := a[i+2] - b[i+2]
		if d2 < 0 {
			d2 = -d2
		}
		d3 := a[i+3] - b[i+3]
		if d3 < 0 {
			d3 = -d3
		}
		sum += d0 + d1 + d2 + d3
	}

	for ; i < dim; i++ {
		diff := a[i] - b[i]
		if diff < 0 {
			diff = -diff
		}
		sum += diff
	}

	return sum
}

func CosineDistance(a, b []float32) float32 {
	return CosineSIMD(a, b)
}

func EuclideanDistance(a, b []float32) float32 {
	return EuclideanSIMD(a, b)
}

func DotProduct(a, b []float32) float32 {
	return DotProductSIMD(a, b)
}

func ManhattanDistance(a, b []float32) float32 {
	return ManhattanSIMD(a, b)
}

func Normalize(vector []float32) []float32 {
	var norm float32
	for _, v := range vector {
		norm += v * v
	}

	if norm == 0 {
		return vector
	}

	norm = float32(math.Sqrt(float64(norm)))
	normalized := make([]float32, len(vector))
	for i, v := range vector {
		normalized[i] = v / norm
	}

	return normalized
}

func NormalizeInPlace(vector []float32) {
	var norm float32
	for _, v := range vector {
		norm += v * v
	}

	if norm == 0 {
		return
	}

	norm = float32(math.Sqrt(float64(norm)))
	for i := range vector {
		vector[i] /= norm
	}
}
