package media

import "github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/media/mlow"

type mlowCodec struct {
	enc *mlow.MlowEncoder
	dec *mlow.MlowDecoder
}

func NewMLowCodec(opts CodecOptions) (Codec, error) {
	_ = opts
	return &mlowCodec{
		enc: mlow.NewMlowEncoder(),
		dec: mlow.NewMlowDecoder(),
	}, nil
}

func (c *mlowCodec) Encode(pcm []float32) ([]byte, error) {
	if len(pcm) == 0 {
		return nil, nil
	}
	return c.enc.Encode(pcm)
}

func (c *mlowCodec) Decode(frame []byte) ([]float32, error) {
	return c.dec.Decode(frame)
}

func (c *mlowCodec) FrameSize() int  { return mlowFrameSize }
func (c *mlowCodec) SampleRate() int { return mlowSampleRate }
func (c *mlowCodec) Close()          {}
