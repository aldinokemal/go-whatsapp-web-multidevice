package mlow

import "math"

// cpx is a single-precision complex value.
//
// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/smpl_perc.rs#L318-L343
type cpx struct {
	re, im float32
}

func (a cpx) add(b cpx) cpx {
	return cpx{re: a.re + b.re, im: a.im + b.im}
}

func (a cpx) mul(b cpx) cpx {
	return cpx{
		re: a.re*b.re - a.im*b.im,
		im: a.re*b.im + a.im*b.re,
	}
}

// smallestFactor returns the smallest prime factor of n (>= 2).
func smallestFactor(n int) int {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/smpl_perc.rs#L346-L358
	if n%2 == 0 {
		return 2
	}
	p := 3
	for p*p <= n {
		if n%p == 0 {
			return p
		}
		p += 2
	}
	return n
}

// fftRec is the recursive mixed-radix Cooley-Tukey DFT. sign is -1 forward, +1
// inverse (unnormalized). x holds n inputs at the given stride; out is contiguous.
func fftRec(x []cpx, stride, n int, sign float32, out []cpx) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/smpl_perc.rs#L362-L405
	if n == 1 {
		out[0] = x[0]
		return
	}
	p := smallestFactor(n)
	if p == n {
		for k := 0; k < n; k++ {
			var acc cpx
			angK := sign * 2.0 * smplPI * float32(k) / float32(n)
			for j := 0; j < n; j++ {
				ang := angK * float32(j)
				w := cpx{re: float32(math.Cos(float64(ang))), im: float32(math.Sin(float64(ang)))}
				acc = acc.add(x[j*stride].mul(w))
			}
			out[k] = acc
		}
		return
	}
	m := n / p
	sub := make([]cpx, n)
	for q := 0; q < p; q++ {
		fftRec(x[q*stride:], stride*p, m, sign, sub[q*m:(q+1)*m])
	}
	for k := 0; k < n; k++ {
		kmod := k % m
		var acc cpx
		for q := 0; q < p; q++ {
			ang := sign * 2.0 * smplPI * float32(k) * float32(q) / float32(n)
			tw := cpx{re: float32(math.Cos(float64(ang))), im: float32(math.Sin(float64(ang)))}
			acc = acc.add(sub[q*m+kmod].mul(tw))
		}
		out[k] = acc
	}
}

// cfft computes the complex FFT of a mixed-radix length into out. sign=-1 forward,
// +1 inverse.
func cfft(input, out []cpx, sign float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/smpl_perc.rs#L408-L412
	fftRec(input, 1, len(input), sign, out)
}

// rfftForwardOrdered is the forward real FFT of n real samples, re-packed into the
// ordered REAL layout: f[0]=DC.re, f[1]=Nyquist.re, then [re,im] pairs for bins
// 1..n/2-1. Output length is n.
func rfftForwardOrdered(time, f []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/smpl_perc.rs#L416-L432
	n := len(time)
	cin := make([]cpx, n)
	for i := 0; i < n; i++ {
		cin[i].re = time[i]
	}
	spec := make([]cpx, n)
	cfft(cin, spec, -1.0)
	f[0] = spec[0].re
	f[1] = spec[n/2].re
	for i := 1; i < n/2; i++ {
		f[2*i] = spec[i].re
		f[2*i+1] = spec[i].im
	}
}
