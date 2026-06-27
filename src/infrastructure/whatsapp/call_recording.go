package whatsapp

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/media"
)

const callRecordingFormat = "wav"

type callRecordingMetadata struct {
	Enabled bool   `json:"recording"`
	Path    string `json:"recording_path,omitempty"`
	URL     string `json:"recording_url,omitempty"`
	Format  string `json:"recording_format,omitempty"`
}

type callRecorder struct {
	mu        sync.Mutex
	file      *os.File
	path      string
	local     []float32
	remote    []float32
	dataBytes uint32
	closed    bool
}

func newCallRecorder(path string) (*callRecorder, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("recording path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	recorder := &callRecorder{file: file, path: path}
	if err := recorder.writeHeader(0); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	return recorder, nil
}

func (r *callRecorder) Path() string {
	if r == nil {
		return ""
	}
	return r.path
}

func (r *callRecorder) WriteLocal(pcm []float32) error {
	return r.append(&r.local, pcm)
}

func (r *callRecorder) WriteRemote(pcm []float32) error {
	return r.append(&r.remote, pcm)
}

func (r *callRecorder) append(buffer *[]float32, pcm []float32) error {
	if len(pcm) == 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	*buffer = append(*buffer, append([]float32(nil), pcm...)...)
	return r.flushLocked(false)
}

func (r *callRecorder) flushLocked(force bool) error {
	for len(r.local) > 0 || len(r.remote) > 0 {
		var n int
		switch {
		case len(r.local) > 0 && len(r.remote) > 0:
			if len(r.local) < len(r.remote) {
				n = len(r.local)
			} else {
				n = len(r.remote)
			}
		case len(r.local) > 0:
			if !force {
				return nil
			}
			n = len(r.local)
		case len(r.remote) > 0:
			if !force {
				return nil
			}
			n = len(r.remote)
		default:
			return nil
		}

		mixed := make([]float32, n)
		for i := 0; i < n; i++ {
			var local, remote float32
			if i < len(r.local) {
				local = r.local[i]
			}
			if i < len(r.remote) {
				remote = r.remote[i]
			}
			sample := local + remote
			switch {
			case sample > 1:
				sample = 1
			case sample < -1:
				sample = -1
			}
			mixed[i] = sample
		}

		data := media.PCMFloat32ToInt16LE(mixed)
		if _, err := r.file.Write(data); err != nil {
			return err
		}
		r.dataBytes += uint32(len(data))
		r.local = r.local[n:]
		r.remote = r.remote[n:]
	}
	return nil
}

func (r *callRecorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	if err := r.flushLocked(true); err != nil {
		return err
	}
	if err := r.writeHeader(r.dataBytes); err != nil {
		return err
	}
	r.closed = true
	return r.file.Close()
}

func (r *callRecorder) writeHeader(dataBytes uint32) error {
	if _, err := r.file.Seek(0, 0); err != nil {
		return err
	}
	header := make([]byte, 44)
	copy(header[0:4], []byte("RIFF"))
	binary.LittleEndian.PutUint32(header[4:8], 36+dataBytes)
	copy(header[8:12], []byte("WAVE"))
	copy(header[12:16], []byte("fmt "))
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], 1)
	binary.LittleEndian.PutUint32(header[24:28], 16000)
	binary.LittleEndian.PutUint32(header[28:32], 16000*2)
	binary.LittleEndian.PutUint16(header[32:34], 2)
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], []byte("data"))
	binary.LittleEndian.PutUint32(header[40:44], dataBytes)
	_, err := r.file.Write(header)
	return err
}

func callRecordingPath(deviceID, callID string) string {
	return filepath.Join(config.PathMedia, "calls", sanitizeRecordingComponent(deviceID), sanitizeRecordingComponent(callID)+".wav")
}

func callRecordingURL(path string) string {
	normalized := filepath.ToSlash(strings.TrimPrefix(path, "./"))
	basePath := strings.TrimRight(config.AppBasePath, "/")
	if basePath == "" {
		return "/" + strings.TrimPrefix(normalized, "/")
	}
	return basePath + "/" + strings.TrimPrefix(normalized, "/")
}

func sanitizeRecordingComponent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "call"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "call"
	}
	return b.String()
}

func callRecordingMetadataJSON(info domainCall.CallInfo) string {
	if !info.Recording && info.RecordingPath == "" && info.RecordingURL == "" {
		return info.Metadata
	}
	meta := callRecordingMetadata{
		Enabled: info.Recording,
		Path:    info.RecordingPath,
		URL:     info.RecordingURL,
		Format:  info.RecordingFormat,
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return info.Metadata
	}
	return string(data)
}

func ApplyCallRecordingMetadata(info *domainCall.CallInfo, metadata string) {
	if info == nil || strings.TrimSpace(metadata) == "" {
		return
	}
	var meta callRecordingMetadata
	if err := json.Unmarshal([]byte(metadata), &meta); err != nil {
		return
	}
	info.Recording = meta.Enabled
	info.RecordingPath = meta.Path
	info.RecordingURL = meta.URL
	info.RecordingFormat = meta.Format
}
