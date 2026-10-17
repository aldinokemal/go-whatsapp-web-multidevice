package usecase

import (
	"context"
	"image"
	"image/color"
	"image/gif"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTestGIF(t *testing.T, frames int) string {
	t.Helper()
	g := &gif.GIF{}
	palette := color.Palette{color.Black, color.White}
	for i := 0; i < frames; i++ {
		img := image.NewPaletted(image.Rect(0, 0, 40, 20), palette)
		img.SetColorIndex(i%40, 0, 1)
		g.Image = append(g.Image, img)
		g.Delay = append(g.Delay, 10)
	}
	path := filepath.Join(t.TempDir(), "in.gif")
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, gif.EncodeAll(f, g))
	require.NoError(t, f.Close())
	return path
}

func TestIsAnimatedGIF(t *testing.T) {
	assert.True(t, isAnimatedGIF(writeTestGIF(t, 3)))
	assert.False(t, isAnimatedGIF(writeTestGIF(t, 1)))
	assert.False(t, isAnimatedGIF(filepath.Join(t.TempDir(), "missing.gif")))
}

func TestBuildGIFStickerFFmpegArgs(t *testing.T) {
	args := buildGIFStickerFFmpegArgs("in.gif", "out.webp", 50, 12)
	joined := strings.Join(args, " ")
	assert.Contains(t, joined, "fps=12,scale=512:512")
	assert.Contains(t, joined, "pad=512:512")
	assert.Contains(t, joined, "-q:v 50")
	assert.Contains(t, joined, "-loop 0")
	assert.NotContains(t, args, "-vsync")
	assert.Equal(t, "out.webp", args[len(args)-1])
}

func TestConvertGIFToAnimatedWebP(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	out := filepath.Join(t.TempDir(), "out.webp")
	require.NoError(t, convertGIFToAnimatedWebP(context.Background(), writeTestGIF(t, 5), out))
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(data), 500*1024)
	require.Greater(t, len(data), 30)
	require.Equal(t, "VP8X", string(data[12:16]))
	assert.NotZero(t, data[20]&0x02, "animation flag")
	canvas := func(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 + 1 }
	assert.Equal(t, 512, canvas(data[24:27]))
	assert.Equal(t, 512, canvas(data[27:30]))
}
