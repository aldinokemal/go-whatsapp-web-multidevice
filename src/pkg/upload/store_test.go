package upload

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// onePixelPNG is a real, tiny, fully-decodable PNG - enough for
// http.DetectContentType to sniff it as image/png.
var onePixelPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
	0x42, 0x60, 0x82,
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(t.TempDir())
}

func TestCreateReturnsDistinctUnguessableIDs(t *testing.T) {
	s := newTestStore(t)
	id1, err := s.Create("device-1", time.Minute, 1<<20)
	require.NoError(t, err)
	id2, err := s.Create("device-1", time.Minute, 1<<20)
	require.NoError(t, err)

	assert.NotEqual(t, id1, id2)
	assert.Equal(t, 40, len(id1)) // 20 bytes, hex-encoded
}

func TestUploadThenFetchRoundTrips(t *testing.T) {
	s := newTestStore(t)
	id, err := s.Create("device-1", time.Minute, 1<<20)
	require.NoError(t, err)

	require.NoError(t, s.Upload(id, onePixelPNG))

	path, mimeType, err := s.Fetch(id)
	require.NoError(t, err)
	assert.Equal(t, "image/png", mimeType)
	got, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, onePixelPNG, got)
}

func TestFetchCanBeRetriedBeforeExpiry(t *testing.T) {
	s := newTestStore(t)
	id, err := s.Create("device-1", time.Minute, 1<<20)
	require.NoError(t, err)
	require.NoError(t, s.Upload(id, onePixelPNG))

	_, _, err1 := s.Fetch(id)
	_, _, err2 := s.Fetch(id)
	require.NoError(t, err1)
	require.NoError(t, err2)
}

func TestFetchRefusesBeforeUpload(t *testing.T) {
	s := newTestStore(t)
	id, err := s.Create("device-1", time.Minute, 1<<20)
	require.NoError(t, err)

	_, _, err = s.Fetch(id)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestUploadRefusesUnknownID(t *testing.T) {
	s := newTestStore(t)
	err := s.Upload("not-a-real-ticket", onePixelPNG)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestUploadRefusesASecondUpload(t *testing.T) {
	s := newTestStore(t)
	id, err := s.Create("device-1", time.Minute, 1<<20)
	require.NoError(t, err)

	require.NoError(t, s.Upload(id, onePixelPNG))
	err = s.Upload(id, onePixelPNG)
	assert.ErrorIs(t, err, ErrAlreadyUploaded)
}

func TestUploadRefusesAnOversizedBody(t *testing.T) {
	s := newTestStore(t)
	id, err := s.Create("device-1", time.Minute, int64(len(onePixelPNG)-1))
	require.NoError(t, err)

	err = s.Upload(id, onePixelPNG)
	assert.ErrorIs(t, err, ErrTooLarge)
}

func TestUploadRefusesAnUnsupportedType(t *testing.T) {
	s := newTestStore(t)
	id, err := s.Create("device-1", time.Minute, 1<<20)
	require.NoError(t, err)

	err = s.Upload(id, []byte("#!/bin/sh\necho not an image\n"))
	assert.ErrorIs(t, err, ErrUnsupportedType)
}

func TestUploadRefusesAnExpiredTicket(t *testing.T) {
	s := newTestStore(t)
	id, err := s.Create("device-1", -time.Second, 1<<20)
	require.NoError(t, err)

	err = s.Upload(id, onePixelPNG)
	assert.ErrorIs(t, err, ErrExpired)
}

func TestFetchRefusesAnExpiredTicket(t *testing.T) {
	s := newTestStore(t)
	id, err := s.Create("device-1", 10*time.Millisecond, 1<<20)
	require.NoError(t, err)
	require.NoError(t, s.Upload(id, onePixelPNG))
	path, _, err := s.Fetch(id)
	require.NoError(t, err)

	time.Sleep(30 * time.Millisecond)

	_, _, err = s.Fetch(id)
	assert.ErrorIs(t, err, ErrExpired)

	// The regression this guards: Fetch used to delete only the map entry,
	// leaving the file on disk with nothing left to ever find and remove it
	// (Sweep only looks at tickets still in the map) - a permanently
	// orphaned file, not merely a late one.
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "an expired ticket's file must not outlive the ticket that owned it")
}

func TestUploadOnAnExpiredAlreadyUploadedTicketCleansUpTheFile(t *testing.T) {
	s := newTestStore(t)
	id, err := s.Create("device-1", 10*time.Millisecond, 1<<20)
	require.NoError(t, err)
	require.NoError(t, s.Upload(id, onePixelPNG))
	path, _, err := s.Fetch(id)
	require.NoError(t, err)

	time.Sleep(30 * time.Millisecond)

	// A second PUT after expiry hits Upload's own expiry check before its
	// "already uploaded" check; that path must clean up the file too.
	err = s.Upload(id, onePixelPNG)
	assert.ErrorIs(t, err, ErrExpired)

	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
}

func TestCreateRefusesBeyondTheOutstandingTicketCeiling(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < maxOutstandingTickets; i++ {
		_, err := s.Create("device-1", time.Minute, 1<<20)
		require.NoError(t, err)
	}

	_, err := s.Create("device-1", time.Minute, 1<<20)
	assert.ErrorIs(t, err, ErrTooManyTickets)
}

func TestClearDirectoryRemovesLeftoverFilesButNotSubdirectories(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.png"), onePixelPNG, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.jpg"), onePixelPNG, 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "a-subdir"), 0o755))

	require.NoError(t, ClearDirectory(dir))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "a-subdir", entries[0].Name())
}

func TestClearDirectoryOnAMissingDirectoryIsNotAnError(t *testing.T) {
	assert.NoError(t, ClearDirectory(filepath.Join(t.TempDir(), "never-created")))
}

func TestSweepRemovesExpiredTicketsAndTheirFiles(t *testing.T) {
	s := newTestStore(t)
	id, err := s.Create("device-1", 10*time.Millisecond, 1<<20)
	require.NoError(t, err)
	require.NoError(t, s.Upload(id, onePixelPNG))
	path, _, err := s.Fetch(id)
	require.NoError(t, err)

	time.Sleep(30 * time.Millisecond)
	s.Sweep()

	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "sweep must delete the uploaded file, not just forget the ticket")

	_, _, err = s.Fetch(id)
	assert.ErrorIs(t, err, ErrNotFound, "a swept ticket is gone entirely, not merely expired")
}

func TestUploadReleasesItsClaimWhenTheDiskWriteFails(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "does-not-exist"))
	id, err := s.Create("device-1", time.Minute, 1<<20)
	require.NoError(t, err)

	err = s.Upload(id, onePixelPNG)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrAlreadyUploaded, "a failed write must not leave the ticket claimed")

	_, _, fetchErr := s.Fetch(id)
	assert.ErrorIs(t, fetchErr, ErrNotFound, "nothing was actually stored, so Fetch must say so")
}
