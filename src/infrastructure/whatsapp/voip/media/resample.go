package media

func NormalizeFrame(pcm []float32, n int) []float32 {
	if len(pcm) == n {
		return pcm
	}
	out := make([]float32, n)
	copy(out, pcm)
	return out
}
