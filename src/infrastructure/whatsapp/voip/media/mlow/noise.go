package mlow

import (
	"math"
	"sync"
)

// CELP decoder-side noise generator: builds the shaped residual noise the CELP
// synthesis mixes into the excitation (smpl_gennoise.rs). The perceptual-weighting
// front-end and bitrate controller the datasheet also bundles are encoder/analysis
// concerns and are scaffolded with the encoder module, not here.

const (
	smplMaxSFLen       = 160
	smplNoiseCorrOrder = 2
	smplNoiseDCTOrder  = 16
	smplCelpFsKHz      = 16
	smplPiNoise        = float32(3.1415926535897)

	decNoiseVNoiseGain  = float32(0.35)
	decNoiseUVNoiseGain = float32(0.8)
	decNoiseUVFcornerHz = float32(800.0)
	envSmthCoefV        = float32(0.95)
	envSmthCoefUV       = float32(0.995)
	envSmthCoefUVV      = float32(0.99)
)

var coefMAV = [3]float32{0.25, -0.496, 0.25}

// NoiseGenerator is the persistent decoder-side noise generator state.
type NoiseGenerator struct {
	EnvSmth       float32
	EnvLast       float32
	OutStateUV    [2]float32
	OutStateV     [2]float32
	CorrSmth      [smplNoiseCorrOrder + 1]float32
	ShapeState    [smplNoiseCorrOrder]float32
	PrevVoiced    bool
	SinceUnvoiced int32
	RandSeed      int32
}

// NewNoiseGenerator allocates a zeroed noise generator.
func NewNoiseGenerator() *NoiseGenerator {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_gennoise.rs#L359-L374
	return &NoiseGenerator{}
}

// smpl_RAND: LCG, wrapping i32 arithmetic (907633515 + (u32)seed*196314165).
func smplRand(seed int32) int32 {
	return int32(907633515) + int32(uint32(seed)*196314165)
}

// smpl_sigmoid with the same +/-80 clamp as C.
func smplSigmoid(x float32) float32 {
	if x > 80.0 {
		return 1.0
	}
	if x < -80.0 {
		return 0.0
	}
	return 1.0 / (1.0 + float32(math.Exp(float64(-x))))
}

func smplNrg(x []float32) float32 {
	var nrg float32
	for _, v := range x {
		nrg += v * v
	}
	return nrg
}

func smplSum(x []float32) float32 {
	var s float32
	for _, v := range x {
		s += v
	}
	return s
}

func smplMaximum(x []float32) float32 {
	m := x[0]
	for _, v := range x[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

// smpl_gen_rand_pulses: 4-at-a-time bit-rotated white pulses scaled by 8.1e-10.
func smplGenRandPulses(noise []float32, l int, seed *int32) {
	const sc = float32(8.1e-10)
	i := 0
	for i+3 < l {
		*seed = smplRand(*seed)
		s := uint32(*seed)
		noise[i] = sc * float32(*seed)
		noise[i+1] = sc * float32(int32(s<<8))
		noise[i+2] = sc * float32(int32(s<<16))
		noise[i+3] = sc * float32(int32(s<<24))
		i += 4
	}
	for i < l {
		*seed = smplRand(*seed)
		noise[i] = sc * float32(*seed)
		i++
	}
}

// smpl_get_env: squared-signal smoothing envelope (4-wide, mirrors the C order).
func smplGetEnv(exc []float32, length int, smthCoef float32, smthState *float32, env []float32) {
	smthCoef *= smthCoef // operate on squared signal
	state := *smthState + 1e-8
	state *= state
	gainCoef := 1.0 - smthCoef
	smthCoef2 := smthCoef * smthCoef
	gainSmthCoef := gainCoef * smthCoef
	i := 0
	for i+3 < length {
		tmp0 := float32(exc[i]*exc[i]) + float32(exc[i+1]*exc[i+1])
		tmp1 := float32(exc[i+2]*exc[i+2]) + float32(exc[i+3]*exc[i+3])
		y1 := float32(gainCoef*tmp1) + float32(gainSmthCoef*tmp0) + float32(smthCoef2*state)
		y0 := float32(gainCoef*tmp0) + float32(smthCoef*state)
		env[i] = float32(math.Sqrt(float64(y0)))
		env[i+1] = env[i]
		env[i+2] = float32(math.Sqrt(float64(y1)))
		env[i+3] = env[i+2]
		state = y1
		i += 4
	}
	*smthState = env[length-1]
}

// smpl_get_env0: decaying envelope when there is no excitation to seed from.
func smplGetEnv0(length int, smthCoef float32, smthState *float32, env []float32) {
	smthCoef2 := smthCoef * smthCoef
	env[0] = (*smthState + 1e-8) * smthCoef
	env[1] = env[0]
	i := 2
	for i+2 < length {
		env[i+2] = env[i-1] * smthCoef2
		env[i+3] = env[i+2]
		env[i] = env[i-1] * smthCoef
		env[i+1] = env[i]
		i += 4
	}
	env[length-2] = env[length-3] * smthCoef
	env[length-1] = env[length-2]
	*smthState = env[length-1]
}

// smpl_filt_ma1 (coef_len=2, state_len=1). x != y.
func smplFiltMA1(x []float32, n int, coef [2]float32, state *float32, y []float32) {
	if coef[0] == 1.0 {
		for k := 1; k < n; k++ {
			y[k] = x[k] + coef[1]*x[k-1]
		}
	} else {
		for k := 0; k < n; k++ {
			y[k] = coef[0] * x[k]
		}
		for k := 1; k < n; k++ {
			y[k] += coef[1] * x[k-1]
		}
	}
	y[0] = coef[0]*x[0] + coef[1]*(*state)
	*state = x[n-1]
}

// smpl_filt_ar1 (coef_len=2, state_len=1, coef[0]==1).
func smplFiltAR1(x []float32, n int, coef [2]float32, state *float32, y []float32) {
	ar1 := -coef[1]
	ytmp := *state
	for nn := 0; nn < n; nn++ {
		ytmp = x[nn] + ytmp*ar1
		y[nn] = ytmp
	}
	*state = ytmp
}

// smpl_filt_arma1: MA1 then AR1, state {ma, ar}.
func smplFiltARMA1(x []float32, n int, coefMA, coefAR [2]float32, state *[2]float32, y []float32) {
	var tmp [smplMaxSFLen]float32
	maState := state[0]
	smplFiltMA1(x, n, coefMA, &maState, tmp[:])
	state[0] = maState
	arState := state[1]
	smplFiltAR1(tmp[:], n, coefAR, &arState, y)
	state[1] = arState
}

// smpl_filt_ma2 (coef_len=3, state_len=2). x != y.
func smplFiltMA2(x []float32, n int, coef [3]float32, state *[2]float32, y []float32) {
	if coef[0] == 1.0 {
		for i := 1; i < n; i++ {
			y[i] = x[i] + coef[1]*x[i-1]
		}
	} else {
		for i := 0; i < n; i++ {
			y[i] = coef[0] * x[i]
		}
		for i := 1; i < n; i++ {
			y[i] += coef[1] * x[i-1]
		}
	}
	for i := 2; i < n; i++ {
		y[i] += coef[2] * x[i-2]
	}
	y[0] = coef[0]*x[0] + coef[1]*state[0] + coef[2]*state[1]
	y[1] += coef[2] * state[0]
	state[0] = x[n-1]
	state[1] = x[n-2]
}

// smpl_spec_fact2: spectral factorization of a 3-tap autocorrelation into a 3-tap MA.
func smplSpecFact2(cIn [3]float32, a *[3]float32) {
	c := cIn
	c[0] += 1e-30
	invC0 := 1.0 / c[0]
	r2 := c[2] * invC0
	r1 := c[1] / (c[0] * (1.0 + r2))
	for iter := 0; iter < 2; iter++ {
		v0 := 1.0 + r1*r1 + r2*r2
		v1 := r1 + r1*r2
		s := -2.0 / v0
		da0 := s * r1
		da1 := s * r2
		s = v0 * invC0
		e1 := s*c[1] - v1
		e2 := s*c[2] - r2
		r0 := 2.0*r1 + v0*da0
		r3 := 2.0*r2 + v0*da1
		rr00 := r0 * r0
		rr01 := r0 * r3
		rr11 := r3 * r3
		rcap1 := 1.0 + r2 + v1*da0
		r4 := r1 + v1*da1
		rr00 += rcap1 * rcap1
		rr01 += rcap1 * r4
		rr11 += r4 * r4
		re0 := rcap1 * e1
		re1 := r4 * e1
		r2c := r2 * da0
		r5 := 1.0 + r2*da1
		rr00 += r2c * r2c
		rr01 += r2c * r5
		rr11 += r5 * r5
		re0 += r2c * e2
		re1 += r5 * e2
		s = rr00*rr11 - rr01*rr01
		if s < 1e-4 {
			break
		}
		s = 1.0 / s
		r1 += (rr11*re0 - rr01*re1) * s
		r2 += (-rr01*re0 + rr00*re1) * s
	}
	sc := float32(math.Sqrt(float64(c[0] / (1.0 + r1*r1 + r2*r2))))
	a[0] = sc
	a[1] = sc * r1
	a[2] = sc * r2
}

// noiseDCT builds the noise DCT matrix (dct_mat_t[CORR+1][DCT_ORDER]), once.
func noiseDCT() *[smplNoiseCorrOrder + 1][smplNoiseDCTOrder]float32 {
	noiseDCTOnce.Do(func() {
		sc := 1.0 / float32(math.Sqrt(float64(smplNoiseDCTOrder)))
		for i := 0; i < smplNoiseDCTOrder; i++ {
			dOmega := ((0.5 + float32(i)) * smplPiNoise) / float32(smplNoiseDCTOrder)
			var omega float32
			for j := 0; j < smplNoiseCorrOrder+1; j++ {
				noiseDCTMat[j][i] = float32(math.Cos(float64(omega))) * sc
				omega += dOmega
			}
		}
	})
	return &noiseDCTMat
}

var (
	noiseDCTOnce sync.Once
	noiseDCTMat  [smplNoiseCorrOrder + 1][smplNoiseDCTOrder]float32
)

// noiseMatMultTransp16: y[0..16] = sum_j C[j][i]*x[j].
func noiseMatMultTransp16(c *[smplNoiseCorrOrder + 1][smplNoiseDCTOrder]float32, x, y []float32, lenX int) {
	var yt [smplNoiseDCTOrder]float32
	xtmp := x[0]
	for i := 0; i < smplNoiseDCTOrder; i++ {
		yt[i] = c[0][i] * xtmp
	}
	for j := 1; j < lenX; j++ {
		xt := x[j]
		for i := 0; i < smplNoiseDCTOrder; i++ {
			yt[i] += c[j][i] * xt
		}
	}
	copy(y[:smplNoiseDCTOrder], yt[:])
}

// noiseMatMult: y[i] = dot(C[i], x) over DCT_ORDER, for i in 0..CORR+1.
func noiseMatMult(c *[smplNoiseCorrOrder + 1][smplNoiseDCTOrder]float32, x, y []float32) {
	for i := 0; i < smplNoiseCorrOrder+1; i++ {
		var acc float32
		for k := 0; k < smplNoiseDCTOrder; k++ {
			acc += c[i][k] * x[k]
		}
		y[i] = acc
	}
}

// SmplGetNormalizedBitrate maps the per-frame pulse count to the normalized bitrate.
func SmplGetNormalizedBitrate(numPulses, frameLength16 int32) float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_gennoise.rs#L329-L332
	pulsesPer20ms := float32(numPulses*frameLength16) / (20.0 * 16.0)
	return smplSigmoid(1.4*float32(math.Log2(float64(pulsesPer20ms+1.0))) - 6.5)
}

// SmplDecodeResnrg maps the quantized residual-energy floor to a linear residual energy.
func SmplDecodeResnrg(nrgresFrameDbqQ14, fcbSubfrlen int32) float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_gennoise.rs#L336-L343
	exp := 0.1 * (float32(nrgresFrameDbqQ14) / float32(int32(1)<<14))
	resnrg := float32(math.Pow(10, float64(exp))) - smplResNrgBias
	if resnrg < 0.0 {
		resnrg = 0.0
	}
	return resnrg * float32(fcbSubfrlen)
}

// add_noise_uv: HP-shape the unvoiced noise and add it into noise.
func addNoiseUV(ng *NoiseGenerator, excNoiseUV []float32, l int, lsf []float32, nrgRatio float32, noise []float32) {
	lsfHz := 16000.0 * (lsf[0] + lsf[1]) / (4.0 * smplPiNoise)
	minUVFcornerHz := lsfHz * 3.0 * smplSigmoid(0.2/(lsf[1]-lsf[0]+1e-30)-3.0)
	uvFcornerHz := decNoiseUVFcornerHz * minF32(0.6+0.4*nrgRatio, 1.0)
	uvFcornerHz = maxF32(uvFcornerHz, minUVFcornerHz)
	uvFcornerHz = minF32(uvFcornerHz, 1500.0)
	coefTmp := 6.0 * uvFcornerHz / 16000.0
	g := (1.0 - 0.5*coefTmp) * decNoiseUVNoiseGain
	coefMAUV := [2]float32{g, -g}
	coefARUV := [2]float32{1.0, -1.0 + coefTmp}
	var filtered [smplMaxSFLen]float32
	smplFiltARMA1(excNoiseUV, l, coefMAUV, coefARUV, &ng.OutStateUV, filtered[:])
	copy(excNoiseUV[:l], filtered[:l])
	for i := 0; i < l; i++ {
		noise[i] += excNoiseUV[i]
	}
}

func minF32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

// SmplCelpGenNoise builds the shaped residual noise for one subframe (writes l
// samples into noise).
func SmplCelpGenNoise(ng *NoiseGenerator, excLpc []float32, l int, voiced bool, numPulses int32, nrgres float32, fcbgIdx int32, lsf []float32, normalizedBitrate float32, fcbgainsUV []float32, noise []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_gennoise.rs#L416-L611
	nrgRatio := float32(1.0)
	var noiseUV, noiseV, noiseV2, env [smplMaxSFLen]float32

	if voiced {
		var corrs, c, ctgt [smplNoiseCorrOrder + 1]float32
		for i := 0; i < smplNoiseCorrOrder+1; i++ {
			var acc float32
			for k := 0; k < l-i; k++ {
				acc += excLpc[k] * excLpc[k+i]
			}
			corrs[i] = acc
		}
		corrs[0] += 1e-12
		corrSmthCoef := float32(0.16)
		if l == smplCelpFsKHz*10 {
			corrSmthCoef = 0.4
		}
		for i := 0; i < smplNoiseCorrOrder+1; i++ {
			ng.CorrSmth[i] += corrSmthCoef * (corrs[i] - ng.CorrSmth[i])
		}
		scale := decNoiseVNoiseGain * decNoiseVNoiseGain * corrs[0] / ng.CorrSmth[0]
		for i := 0; i < smplNoiseCorrOrder+1; i++ {
			c[i] = ng.CorrSmth[i] * scale
		}
		c[1] *= 2.0
		c[2] *= 2.0

		dct := noiseDCT()
		var f2, f2Tgt [smplNoiseDCTOrder]float32
		noiseMatMultTransp16(dct, c[:], f2[:], smplNoiseCorrOrder+1)
		m := smplMaximum(f2[:smplNoiseDCTOrder]) * 1.5
		for i := 0; i < smplNoiseDCTOrder; i++ {
			f2Tgt[i] = m - f2[i]
		}
		noiseMatMult(dct, f2Tgt[:], ctgt[:])
		smplGenRandPulses(noiseV[:], l, &ng.RandSeed)
		if !ng.PrevVoiced {
			ng.EnvSmth = ng.EnvLast
		}
		smplGetEnv(excLpc, l, envSmthCoefV, &ng.EnvSmth, env[:])
		for i := 0; i < l; i++ {
			noiseV[i] *= env[i]
		}
		nrgNoise := smplNrg(noiseV[:l])
		inv := 1.0 / (nrgNoise + 1e-12)
		for i := 0; i < smplNoiseCorrOrder+1; i++ {
			ctgt[i] *= inv
		}
		var coefMA [smplNoiseCorrOrder + 1]float32
		smplSpecFact2(ctgt, &coefMA)
		smplFiltMA2(noiseV[:], l, coefMA, &ng.ShapeState, noiseV2[:])

		if !ng.PrevVoiced {
			smplGenRandPulses(noiseUV[:], l, &ng.RandSeed)
			envVal := ng.EnvLast * envSmthCoefUVV
			for i := 0; i < l; i += 2 {
				noiseUV[i] *= envVal
				noiseUV[i+1] *= envVal * envSmthCoefUVV
				envVal *= envSmthCoefUVV * envSmthCoefUVV
			}
		} else if ng.SinceUnvoiced < 2 {
			for i := 0; i < l; i++ {
				noiseUV[i] = 0.0
			}
		}
		ng.EnvLast = env[l-1]
	} else {
		for i := range ng.CorrSmth {
			ng.CorrSmth[i] = 0.0
		}
		for i := range ng.ShapeState {
			ng.ShapeState[i] = 0.0
		}
		for i := 0; i < l; i++ {
			noiseV2[i] = 0.0
		}

		var nrgTgt float32
		if numPulses > 0 {
			nrgRatio = smplNrg(excLpc[:l]) / (nrgres + 1e-20)
			hardness := 10.0 + 20.0*normalizedBitrate
			nrgTgt = nrgres * float32(math.Log(float64(float32(math.Exp(float64(hardness*(1.0-nrgRatio))))+1.0))) / hardness
			smplGetEnv(excLpc, l, envSmthCoefUV, &ng.EnvSmth, env[:])
		} else {
			nrgRatio = 0.0
			nrgTgt = nrgres
			smplGetEnv0(l, envSmthCoefUV, &ng.EnvSmth, env[:])
		}

		scale := 1.0 / float32(l)
		nrgTgt = nrgTgt*scale + 1e-30
		nrgEnv := smplNrg(env[:l]) * scale
		f := float32(math.Sqrt(float64(nrgTgt)))
		gg := float32(math.Sqrt(float64(nrgTgt / nrgEnv)))
		ge := gg * env[0]
		envLast := ng.EnvLast
		if envLast < minF32(f, ge) {
			if f < ge {
				gg = 0.0
			} else {
				f = 0.0
			}
		} else if envLast > maxF32(f, ge) {
			if f > ge {
				gg = 0.0
			} else {
				f = 0.0
			}
		} else {
			sumEnv := smplSum(env[:l]) * scale
			a := nrgEnv + env[0]*env[0] - 2.0*sumEnv*env[0]
			b := 2.0 * envLast * (sumEnv - env[0])
			cc := envLast*envLast - nrgTgt
			tmp := b*b - 4.0*a*cc
			if tmp < 1e-35 || a < 1e-25 {
				f = 0.0
				gg = 0.0
			} else {
				tmp = float32(math.Sqrt(float64(tmp)))
				scale = 0.5 / a
				gg = (-b + tmp) * scale
				f = envLast - env[0]*gg
				if f < 0.0 {
					gg = (-b - tmp) * scale
					f = envLast - env[0]*gg
				}
			}
		}

		smplGenRandPulses(noiseUV[:], l, &ng.RandSeed)
		if numPulses > 0 {
			maxVal := fcbgainsUV[fcbgIdx] * 0.5
			for i := 0; i < l; i++ {
				if excLpc[i] == 0.0 {
					noiseUV[i] *= minF32(f+gg*env[i], maxVal)
				} else {
					noiseUV[i] = 0.0
				}
			}
			ng.EnvLast = minF32(f+gg*env[l-1], maxVal)
		} else {
			for i := 0; i < l; i++ {
				noiseUV[i] *= f + gg*env[i]
			}
			ng.EnvLast = f + gg*env[l-1]
		}
	}

	if ng.PrevVoiced || voiced {
		smplFiltMA2(noiseV2[:], l, coefMAV, &ng.OutStateV, noise)
	} else {
		for i := 0; i < l; i++ {
			noise[i] = 0.0
		}
	}
	if ng.SinceUnvoiced < 2 || !voiced {
		addNoiseUV(ng, noiseUV[:], l, lsf, nrgRatio, noise)
	} else {
		ng.OutStateUV = [2]float32{0.0, 0.0}
	}
	ng.PrevVoiced = voiced
	if voiced {
		ng.SinceUnvoiced++
	} else {
		ng.SinceUnvoiced = 0
	}
}
