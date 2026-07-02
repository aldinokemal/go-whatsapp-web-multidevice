package whatsapp

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallRecorderWritesMixedWAV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "call.wav")

	recorder, err := newCallRecorder(path)
	require.NoError(t, err)

	recorder.local = []float32{0.25}
	recorder.remote = []float32{0.25}
	require.NoError(t, recorder.flushLocked(true))
	require.NoError(t, recorder.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Greater(t, len(data), 44)

	assert.Equal(t, "RIFF", string(data[0:4]))
	assert.Equal(t, "WAVE", string(data[8:12]))
	assert.Equal(t, "fmt ", string(data[12:16]))
	assert.Equal(t, "data", string(data[36:40]))

	sample := int16(binary.LittleEndian.Uint16(data[44:46]))
	assert.InDelta(t, 16383, sample, 2)
}

func TestCallRecorderFlushesOneSidedAudio(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "call.wav")

	recorder, err := newCallRecorder(path)
	require.NoError(t, err)

	require.NoError(t, recorder.WriteLocal([]float32{0.25, 0.25}))
	require.Empty(t, recorder.local)
	require.NoError(t, recorder.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Greater(t, len(data), 44)

	first := int16(binary.LittleEndian.Uint16(data[44:46]))
	second := int16(binary.LittleEndian.Uint16(data[46:48]))
	assert.InDelta(t, 8191, first, 2)
	assert.InDelta(t, 8191, second, 2)
}
