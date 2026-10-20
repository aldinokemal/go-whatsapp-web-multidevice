package whatsapp

import (
	"encoding/binary"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/purpshell/meowcaller"
	"github.com/stretchr/testify/require"
)

type stubAudioSink struct{ closed bool }

func (s *stubAudioSink) WriteFrame([]float32) error { return nil }

func (s *stubAudioSink) Close() error {
	s.closed = true
	return nil
}

func TestChannelAudioSourceFramesMicrophoneBytes(t *testing.T) {
	raw := make([]byte, 0, meowcaller.FrameSamples*2)
	for i := 0; i < meowcaller.FrameSamples; i++ {
		raw = binary.LittleEndian.AppendUint16(raw, uint16(int16(i)))
	}

	frames := make(chan []byte, 1)
	frames <- raw

	frame, err := newChannelAudioSource(frames).ReadFrame()
	require.NoError(t, err)
	require.Len(t, frame, meowcaller.FrameSamples)
	require.InDelta(t, 0, frame[0], 0.0001)
	require.InDelta(t, float32(500)/float32(32767), frame[500], 0.0001)
}

func TestChannelAudioSourcePadsPartialFrameAndEnds(t *testing.T) {
	frames := make(chan []byte, 1)
	frames <- []byte{0x10, 0x00, 0x20, 0x00} // two s16le samples, then the mic closes
	close(frames)

	source := newChannelAudioSource(frames)
	frame, err := source.ReadFrame()
	require.NoError(t, err)
	require.InDelta(t, float32(16)/float32(32767), frame[0], 0.0001)
	require.InDelta(t, float32(32)/float32(32767), frame[1], 0.0001)
	require.Zero(t, frame[2])

	_, err = source.ReadFrame()
	require.ErrorIs(t, err, io.EOF)
}

func TestChannelAudioSourceEmitsSilenceWhileMicIsQuiet(t *testing.T) {
	source := newChannelAudioSource(make(chan []byte))

	start := time.Now()
	frame, err := source.ReadFrame()

	require.NoError(t, err)
	require.GreaterOrEqual(t, time.Since(start), liveAudioFrameWait/2)
	for _, sample := range frame {
		require.Zero(t, sample)
	}
}

func TestCallerForgetClosesSinkAndRunsEndHooks(t *testing.T) {
	caller := &Caller{
		calls:    map[string]*meowcaller.Call{"CALL1": {}},
		sinks:    map[string]meowcaller.AudioSink{},
		endHooks: map[string][]func(){},
		live:     map[string]bool{"CALL1": true},
	}
	sink := &stubAudioSink{}
	caller.setSink("CALL1", sink)

	var broadcasts []IncomingCallUIBroadcast
	t.Cleanup(func() { SetIncomingCallUIListener(nil) })
	SetIncomingCallUIListener(func(notification IncomingCallUIBroadcast) {
		broadcasts = append(broadcasts, notification)
	})

	ended := 0
	caller.WatchCallEnd("CALL1", func() { ended++ })
	caller.forget("CALL1")

	require.Equal(t, 1, ended)
	require.True(t, sink.closed)
	require.Empty(t, caller.calls)
	require.Empty(t, caller.sinks)
	require.Empty(t, caller.endHooks)
	require.False(t, caller.usesLiveAudio("CALL1"))
	require.Len(t, broadcasts, 1)
	require.Equal(t, IncomingCallEventEnded, broadcasts[0].Event)
	require.Equal(t, "CALL1", broadcasts[0].CallID)

	notified := false
	caller.WatchCallEnd("GONE", func() { notified = true })
	require.True(t, notified)
}

func TestOpenCallRecorderValidatesWavPath(t *testing.T) {
	recorder, err := openCallRecorder("   ")
	require.NoError(t, err)
	require.Nil(t, recorder)

	_, err = openCallRecorder(filepath.Join(t.TempDir(), "peer.mp3"))
	require.ErrorContains(t, err, "must end in .wav")

	path := filepath.Join(t.TempDir(), "peer.WAV")
	recorder, err = openCallRecorder(path)
	require.NoError(t, err)
	require.NoError(t, recorder.WriteFrame(make([]float32, meowcaller.FrameSamples)))
	require.NoError(t, recorder.Close())
	require.FileExists(t, path)
}
