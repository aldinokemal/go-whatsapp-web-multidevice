package usecase

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	fiberUtils "github.com/gofiber/utils/v2"
	"github.com/sirupsen/logrus"
)

const (
	scheduleStatusActive    = "active"
	scheduleStatusRunning   = "running"
	scheduleStatusPaused    = "paused"
	scheduleStatusCompleted = "completed"
	scheduleStatusFailed    = "failed"
	scheduleStatusCancelled = "cancelled"
)

type scheduledAssetInput struct {
	Field  string
	Header *multipart.FileHeader
}

type scheduledAsset struct {
	Path        string `json:"path"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type,omitempty"`
}

type ScheduleService struct {
	repo      domainChatStorage.IChatStorageRepository
	base      domainSend.ISendUsecase
	manager   *whatsapp.DeviceManager
	mediaRoot string
	now       func() time.Time
	once      sync.Once
}

func NewScheduleService(repo domainChatStorage.IChatStorageRepository, base domainSend.ISendUsecase, manager *whatsapp.DeviceManager, mediaRoot string) *ScheduleService {
	return &ScheduleService{repo: repo, base: base, manager: manager, mediaRoot: mediaRoot, now: func() time.Time { return time.Now().UTC() }}
}

func (s *ScheduleService) Create(ctx context.Context, messageType, phone, summary string, options domainSend.ScheduleOptions, request any, assets []scheduledAssetInput) (domainSend.GenericResponse, error) {
	now := s.now()
	spec, err := validations.ParseScheduleOptions(options, now)
	if err != nil {
		return domainSend.GenericResponse{}, err
	}
	if !spec.ScheduledAt.IsZero() {
		spec.ScheduledAt = spec.ScheduledAt.UTC()
	}
	if err := validateScheduledPayload(ctx, messageType, request); err != nil {
		return domainSend.GenericResponse{}, err
	}
	jobID := fiberUtils.UUIDv4()
	assetMeta, err := s.persistAssets(jobID, assets)
	if err != nil {
		return domainSend.GenericResponse{}, err
	}
	cleanupOnError := true
	defer func() {
		if cleanupOnError {
			_ = os.RemoveAll(filepath.Join(s.mediaRoot, "scheduled", jobID))
		}
	}()
	payload, err := json.Marshal(request)
	if err != nil {
		return domainSend.GenericResponse{}, fmt.Errorf("serialize scheduled send: %w", err)
	}
	assetsJSON, err := json.Marshal(assetMeta)
	if err != nil {
		return domainSend.GenericResponse{}, fmt.Errorf("serialize scheduled media: %w", err)
	}
	weekdaysJSON, _ := json.Marshal(spec.Weekdays)
	job := &domainChatStorage.ScheduledSend{
		ID:              jobID,
		DeviceID:        scheduleDeviceID(ctx),
		MessageType:     messageType,
		PayloadJSON:     string(payload),
		AssetsJSON:      string(assetsJSON),
		Phone:           phone,
		Summary:         summary,
		ScheduledAt:     spec.ScheduledAt,
		NextRunAt:       spec.ScheduledAt,
		Timezone:        spec.Timezone,
		Recurrence:      spec.Recurrence,
		WeekdaysJSON:    string(weekdaysJSON),
		DayOfMonth:      spec.DayOfMonth,
		EndAt:           spec.EndAt,
		OccurrenceLimit: spec.OccurrenceLimit,
		Status:          scheduleStatusActive,
	}
	if job.DeviceID == "" {
		return domainSend.GenericResponse{}, fmt.Errorf("device identification required")
	}
	if err := s.repo.CreateScheduledSend(job); err != nil {
		return domainSend.GenericResponse{}, fmt.Errorf("create scheduled send: %w", err)
	}
	cleanupOnError = false
	return domainSend.GenericResponse{
		Status:      "Message scheduled",
		ScheduleID:  job.ID,
		ScheduledAt: job.ScheduledAt.Format(time.RFC3339),
		NextRunAt:   job.NextRunAt.Format(time.RFC3339),
	}, nil
}

func validateScheduledPayload(ctx context.Context, messageType string, request any) error {
	var err error
	switch messageType {
	case "text":
		err = validations.ValidateSendMessage(ctx, request.(domainSend.MessageRequest))
	case "image":
		err = validations.ValidateSendImage(ctx, request.(domainSend.ImageRequest))
	case "file":
		err = validations.ValidateSendFile(ctx, request.(domainSend.FileRequest))
	case "video":
		err = validations.ValidateSendVideo(ctx, request.(domainSend.VideoRequest))
	case "audio":
		err = validations.ValidateSendAudio(ctx, request.(domainSend.AudioRequest))
	case "sticker":
		err = validations.ValidateSendSticker(ctx, request.(domainSend.StickerRequest))
	case "contact":
		err = validations.ValidateSendContact(ctx, request.(domainSend.ContactRequest))
	case "link":
		err = validations.ValidateSendLink(ctx, request.(domainSend.LinkRequest))
	case "location":
		err = validations.ValidateSendLocation(ctx, request.(domainSend.LocationRequest))
	case "poll":
		err = validations.ValidateSendPoll(ctx, request.(domainSend.PollRequest))
	case "forward":
		err = validations.ValidateForwardMessage(ctx, request.(domainSend.ForwardRequest))
	}
	return err
}

func (s *ScheduleService) persistAssets(jobID string, inputs []scheduledAssetInput) (map[string]scheduledAsset, error) {
	result := make(map[string]scheduledAsset)
	for _, input := range inputs {
		if input.Header == nil || input.Field == "" {
			continue
		}
		if filepath.Base(input.Header.Filename) != input.Header.Filename {
			return nil, fmt.Errorf("invalid uploaded filename")
		}
		dir := filepath.Join(s.mediaRoot, "scheduled", jobID)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, fmt.Errorf("create scheduled media directory: %w", err)
		}
		path := filepath.Join(dir, input.Field+"-"+input.Header.Filename)
		src, err := input.Header.Open()
		if err != nil {
			return nil, fmt.Errorf("open uploaded media: %w", err)
		}
		dst, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err == nil {
			_, err = io.Copy(dst, src)
		}
		_ = src.Close()
		_ = dst.Close()
		if err != nil {
			return nil, fmt.Errorf("persist uploaded media: %w", err)
		}
		result[input.Field] = scheduledAsset{Path: path, Filename: input.Header.Filename, ContentType: input.Header.Header.Get("Content-Type")}
	}
	return result, nil
}

func (s *ScheduleService) List(ctx context.Context, filter domainSend.ScheduleFilter) (domainSend.ScheduleListResponse, error) {
	scope := domainChatStorage.ScheduledSendFilter{
		DeviceID:    scheduleDeviceID(ctx),
		Status:      filter.Status,
		Search:      filter.Search,
		MessageType: filter.MessageType,
		Limit:       filter.Limit,
		Offset:      filter.Offset,
	}
	response := domainSend.ScheduleListResponse{
		Pagination: domainSend.PaginationResponse{Limit: filter.Limit, Offset: filter.Offset},
	}
	total, err := s.repo.CountScheduledSends(scope)
	if err != nil {
		return response, err
	}
	response.Pagination.Total = total
	jobs, err := s.repo.ListScheduledSends(scope)
	if err != nil {
		return response, err
	}
	result := make([]domainSend.Schedule, 0, len(jobs))
	for _, job := range jobs {
		view, err := scheduleView(job)
		if err != nil {
			return response, err
		}
		result = append(result, view)
	}
	response.Data = result
	return response, nil
}

func (s *ScheduleService) Get(ctx context.Context, id string) (*domainSend.Schedule, error) {
	job, err := s.repo.GetScheduledSend(scheduleDeviceID(ctx), id)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, fmt.Errorf("scheduled send %s not found", id)
	}
	view, err := scheduleView(job)
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func (s *ScheduleService) Pause(ctx context.Context, id string) error {
	return s.changeStatus(ctx, id, scheduleStatusPaused, nil)
}

func (s *ScheduleService) Resume(ctx context.Context, id string) error {
	job, err := s.repo.GetScheduledSend(scheduleDeviceID(ctx), id)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("scheduled send %s not found", id)
	}
	if job.Status != scheduleStatusPaused {
		return fmt.Errorf("scheduled send %s is not paused", id)
	}
	next := job.NextRunAt
	if !next.After(s.now()) {
		next = s.now()
	}
	return s.changeStatus(ctx, id, scheduleStatusActive, &next)
}

func (s *ScheduleService) Cancel(ctx context.Context, id string) error {
	job, err := s.repo.GetScheduledSend(scheduleDeviceID(ctx), id)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("scheduled send %s not found", id)
	}
	if err := s.changeStatus(ctx, id, scheduleStatusCancelled, nil); err != nil {
		return err
	}
	s.cleanupAssets(job)
	return nil
}

func (s *ScheduleService) changeStatus(ctx context.Context, id, status string, nextRunAt *time.Time) error {
	if strings.TrimSpace(scheduleDeviceID(ctx)) == "" {
		return fmt.Errorf("device identification required")
	}
	job, err := s.repo.GetScheduledSend(scheduleDeviceID(ctx), id)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("scheduled send %s not found", id)
	}
	if job.Status == scheduleStatusCompleted || job.Status == scheduleStatusCancelled {
		return fmt.Errorf("scheduled send %s is already terminal", id)
	}
	return s.repo.SetScheduledSendStatus(scheduleDeviceID(ctx), id, status, nextRunAt)
}

func scheduleDeviceID(ctx context.Context) string {
	if instance, ok := whatsapp.DeviceFromContext(ctx); ok && instance != nil {
		return instance.ID()
	}
	return ""
}

func scheduleView(job *domainChatStorage.ScheduledSend) (domainSend.Schedule, error) {
	var weekdays []int
	if strings.TrimSpace(job.WeekdaysJSON) != "" {
		if err := json.Unmarshal([]byte(job.WeekdaysJSON), &weekdays); err != nil {
			return domainSend.Schedule{}, err
		}
	}
	nextRunAt := timePtr(job.NextRunAt)
	if job.Status == scheduleStatusCompleted || job.Status == scheduleStatusFailed || job.Status == scheduleStatusCancelled {
		nextRunAt = nil
	}
	return domainSend.Schedule{
		ID: job.ID, MessageType: job.MessageType, Phone: job.Phone, Summary: job.Summary, Status: job.Status,
		ScheduledAt: job.ScheduledAt, NextRunAt: nextRunAt, Timezone: job.Timezone,
		Recurrence: job.Recurrence, Weekdays: weekdays, DayOfMonth: job.DayOfMonth, EndAt: job.EndAt,
		OccurrenceLimit: job.OccurrenceLimit, OccurrenceCount: job.OccurrenceCount, Attempts: job.Attempts,
		LastRunAt: job.LastRunAt, LastMessageID: job.LastMessageID, LastError: job.LastError,
		CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt,
	}, nil
}

func timePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func (s *ScheduleService) Start(ctx context.Context) {
	s.once.Do(func() {
		s.garbageCollectAssets()
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				s.processDue(ctx)
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	})
}

func (s *ScheduleService) garbageCollectAssets() {
	root := filepath.Join(s.mediaRoot, "scheduled")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	known := make(map[string]struct{})
	records, err := s.repo.ListDeviceRecords()
	if err == nil {
		for _, record := range records {
			jobs, listErr := s.repo.ListScheduledSends(domainChatStorage.ScheduledSendFilter{DeviceID: record.DeviceID})
			if listErr != nil {
				continue
			}
			for _, job := range jobs {
				known[job.ID] = struct{}{}
			}
		}
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, ok := known[entry.Name()]; !ok {
			_ = os.RemoveAll(filepath.Join(root, entry.Name()))
		}
	}
}

func (s *ScheduleService) processDue(ctx context.Context) {
	now := s.now()
	if err := s.repo.FailExpiredScheduledSends(now); err != nil {
		logrus.WithError(err).Warn("scheduled send worker failed to recover interrupted jobs")
	}
	token := fiberUtils.UUIDv4()
	jobs, err := s.repo.ClaimDueScheduledSends(now, 20, now.Add(2*time.Minute), token)
	if err != nil {
		logrus.WithError(err).Error("scheduled send worker failed to claim jobs")
		return
	}
	for _, job := range jobs {
		if err := s.processJob(ctx, job); err != nil {
			logrus.WithError(err).WithField("schedule_id", job.ID).Warn("scheduled send failed")
		}
	}
}

func (s *ScheduleService) processJob(parent context.Context, job *domainChatStorage.ScheduledSend) error {
	if s.manager == nil {
		return s.retry(job, "device manager is unavailable")
	}
	instance, ok := s.manager.GetDevice(job.DeviceID)
	if !ok || instance == nil || instance.GetClient() == nil || !instance.IsConnected() {
		return s.retry(job, "device is offline")
	}
	ctx, cancel := context.WithTimeout(whatsapp.ContextWithDevice(parent, instance), 2*time.Minute)
	defer cancel()
	response, err := s.dispatch(ctx, job)
	if err != nil {
		if errors.Is(err, pkgError.ErrWaCLI) {
			return s.retry(job, err.Error())
		}
		_ = s.repo.FailScheduledSend(job.ID, job.LeaseToken, err.Error())
		s.cleanupAssets(job)
		return err
	}
	count := job.OccurrenceCount + 1
	next, recurring := s.nextRun(job, count)
	status := scheduleStatusCompleted
	if recurring {
		status = scheduleStatusActive
	}
	if err := s.repo.CompleteScheduledSend(job.ID, job.LeaseToken, status, response.MessageID, count, timePtr(next)); err != nil {
		return err
	}
	if status == scheduleStatusCompleted {
		s.cleanupAssets(job)
	}
	return nil
}

func (s *ScheduleService) retry(job *domainChatStorage.ScheduledSend, reason string) error {
	shift := job.Attempts
	if shift > 8 {
		shift = 8
	}
	delay := 15 * time.Second * time.Duration(1<<shift)
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	return s.repo.RetryScheduledSend(job.ID, job.LeaseToken, reason, s.now().Add(delay))
}

func (s *ScheduleService) nextRun(job *domainChatStorage.ScheduledSend, count int) (time.Time, bool) {
	if job.Recurrence == "once" || (job.OccurrenceLimit > 0 && count >= job.OccurrenceLimit) {
		return time.Time{}, false
	}
	location, err := time.LoadLocation(job.Timezone)
	if err != nil {
		return time.Time{}, false
	}
	var weekdays []int
	_ = json.Unmarshal([]byte(job.WeekdaysJSON), &weekdays)
	spec := validations.ScheduleSpec{ScheduledAt: job.ScheduledAt, Timezone: job.Timezone, Location: location, Recurrence: job.Recurrence, Weekdays: weekdays, DayOfMonth: job.DayOfMonth, EndAt: job.EndAt, OccurrenceLimit: job.OccurrenceLimit}
	next, ok := validations.NextScheduleOccurrence(spec, s.now())
	return next, ok
}

func (s *ScheduleService) cleanupAssets(job *domainChatStorage.ScheduledSend) {
	_ = os.RemoveAll(filepath.Join(s.mediaRoot, "scheduled", job.ID))
}

func (s *ScheduleService) dispatch(ctx context.Context, job *domainChatStorage.ScheduledSend) (domainSend.GenericResponse, error) {
	var assets map[string]scheduledAsset
	if strings.TrimSpace(job.AssetsJSON) != "" {
		if err := json.Unmarshal([]byte(job.AssetsJSON), &assets); err != nil {
			return domainSend.GenericResponse{}, err
		}
	}
	cleanup := func() {}
	defer func() { cleanup() }()
	switch job.MessageType {
	case "text":
		var req domainSend.MessageRequest
		if err := json.Unmarshal([]byte(job.PayloadJSON), &req); err != nil {
			return domainSend.GenericResponse{}, err
		}
		return s.base.SendText(ctx, req)
	case "image":
		var req domainSend.ImageRequest
		if err := json.Unmarshal([]byte(job.PayloadJSON), &req); err != nil {
			return domainSend.GenericResponse{}, err
		}
		form, err := hydrateAsset(assets["image"], "image")
		if err != nil {
			return domainSend.GenericResponse{}, err
		}
		if form != nil {
			req.Image = form.Header
			cleanup = form.cleanup
		}
		return s.base.SendImage(ctx, req)
	case "file":
		var req domainSend.FileRequest
		if err := json.Unmarshal([]byte(job.PayloadJSON), &req); err != nil {
			return domainSend.GenericResponse{}, err
		}
		form, err := hydrateAsset(assets["file"], "file")
		if err != nil {
			return domainSend.GenericResponse{}, err
		}
		if form != nil {
			req.File = form.Header
			cleanup = form.cleanup
		}
		return s.base.SendFile(ctx, req)
	case "video":
		var req domainSend.VideoRequest
		if err := json.Unmarshal([]byte(job.PayloadJSON), &req); err != nil {
			return domainSend.GenericResponse{}, err
		}
		form, err := hydrateAsset(assets["video"], "video")
		if err != nil {
			return domainSend.GenericResponse{}, err
		}
		if form != nil {
			req.Video = form.Header
			cleanup = form.cleanup
		}
		return s.base.SendVideo(ctx, req)
	case "audio":
		var req domainSend.AudioRequest
		if err := json.Unmarshal([]byte(job.PayloadJSON), &req); err != nil {
			return domainSend.GenericResponse{}, err
		}
		form, err := hydrateAsset(assets["audio"], "audio")
		if err != nil {
			return domainSend.GenericResponse{}, err
		}
		if form != nil {
			req.Audio = form.Header
			cleanup = form.cleanup
		}
		return s.base.SendAudio(ctx, req)
	case "sticker":
		var req domainSend.StickerRequest
		if err := json.Unmarshal([]byte(job.PayloadJSON), &req); err != nil {
			return domainSend.GenericResponse{}, err
		}
		form, err := hydrateAsset(assets["sticker"], "sticker")
		if err != nil {
			return domainSend.GenericResponse{}, err
		}
		if form != nil {
			req.Sticker = form.Header
			cleanup = form.cleanup
		}
		return s.base.SendSticker(ctx, req)
	case "contact":
		var req domainSend.ContactRequest
		if err := json.Unmarshal([]byte(job.PayloadJSON), &req); err != nil {
			return domainSend.GenericResponse{}, err
		}
		return s.base.SendContact(ctx, req)
	case "link":
		var req domainSend.LinkRequest
		if err := json.Unmarshal([]byte(job.PayloadJSON), &req); err != nil {
			return domainSend.GenericResponse{}, err
		}
		return s.base.SendLink(ctx, req)
	case "location":
		var req domainSend.LocationRequest
		if err := json.Unmarshal([]byte(job.PayloadJSON), &req); err != nil {
			return domainSend.GenericResponse{}, err
		}
		return s.base.SendLocation(ctx, req)
	case "poll":
		var req domainSend.PollRequest
		if err := json.Unmarshal([]byte(job.PayloadJSON), &req); err != nil {
			return domainSend.GenericResponse{}, err
		}
		return s.base.SendPoll(ctx, req)
	case "forward":
		var req domainSend.ForwardRequest
		if err := json.Unmarshal([]byte(job.PayloadJSON), &req); err != nil {
			return domainSend.GenericResponse{}, err
		}
		return s.base.SendForward(ctx, req)
	default:
		return domainSend.GenericResponse{}, fmt.Errorf("unsupported scheduled message type %q", job.MessageType)
	}
}

type hydratedAsset struct {
	Header  *multipart.FileHeader
	cleanup func()
}

// scheduledAssetContentType restores the MIME type captured when the job was
// created. CreateFormFile would stamp every hydrated part as
// application/octet-stream, which send validation rejects.
func scheduledAssetContentType(asset scheduledAsset) string {
	if asset.ContentType != "" {
		return asset.ContentType
	}
	if byExt := mime.TypeByExtension(filepath.Ext(asset.Filename)); byExt != "" {
		return byExt
	}
	return "application/octet-stream"
}

func hydrateAsset(asset scheduledAsset, field string) (*hydratedAsset, error) {
	if asset.Path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(asset.Path)
	if err != nil {
		return nil, fmt.Errorf("read scheduled media: %w", err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", multipart.FileContentDisposition(field, asset.Filename))
	partHeader.Set("Content-Type", scheduledAssetContentType(asset))
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	form, err := multipart.NewReader(bytes.NewReader(body.Bytes()), writer.FormDataContentType()[len("multipart/form-data; boundary="):]).ReadForm(int64(len(data) + 1024))
	if err != nil {
		return nil, err
	}
	files := form.File[field]
	if len(files) == 0 {
		form.RemoveAll()
		return nil, fmt.Errorf("scheduled media field %s missing", field)
	}
	return &hydratedAsset{Header: files[0], cleanup: func() { _ = form.RemoveAll() }}, nil
}

// scheduledSendService preserves ISendUsecase while intercepting only message
// operations that carry a non-empty ScheduleOptions value.
type scheduledSendService struct {
	base      domainSend.ISendUsecase
	scheduler *ScheduleService
}

func NewScheduledSendService(base domainSend.ISendUsecase, scheduler *ScheduleService) domainSend.ISendUsecase {
	return &scheduledSendService{base: base, scheduler: scheduler}
}

func (s *scheduledSendService) SendText(ctx context.Context, req domainSend.MessageRequest) (domainSend.GenericResponse, error) {
	if !req.IsScheduled() {
		return s.base.SendText(ctx, req)
	}
	return s.scheduler.Create(ctx, "text", req.Phone, req.Message, req.ScheduleOptions, req, nil)
}
func (s *scheduledSendService) SendImage(ctx context.Context, req domainSend.ImageRequest) (domainSend.GenericResponse, error) {
	if !req.IsScheduled() {
		return s.base.SendImage(ctx, req)
	}
	return s.scheduler.Create(ctx, "image", req.Phone, req.Caption, req.ScheduleOptions, req, []scheduledAssetInput{{Field: "image", Header: req.Image}})
}
func (s *scheduledSendService) SendFile(ctx context.Context, req domainSend.FileRequest) (domainSend.GenericResponse, error) {
	if !req.IsScheduled() {
		return s.base.SendFile(ctx, req)
	}
	return s.scheduler.Create(ctx, "file", req.Phone, req.Caption, req.ScheduleOptions, req, []scheduledAssetInput{{Field: "file", Header: req.File}})
}
func (s *scheduledSendService) SendVideo(ctx context.Context, req domainSend.VideoRequest) (domainSend.GenericResponse, error) {
	if !req.IsScheduled() {
		return s.base.SendVideo(ctx, req)
	}
	return s.scheduler.Create(ctx, "video", req.Phone, req.Caption, req.ScheduleOptions, req, []scheduledAssetInput{{Field: "video", Header: req.Video}})
}
func (s *scheduledSendService) SendAudio(ctx context.Context, req domainSend.AudioRequest) (domainSend.GenericResponse, error) {
	if !req.IsScheduled() {
		return s.base.SendAudio(ctx, req)
	}
	return s.scheduler.Create(ctx, "audio", req.Phone, "Audio", req.ScheduleOptions, req, []scheduledAssetInput{{Field: "audio", Header: req.Audio}})
}
func (s *scheduledSendService) SendSticker(ctx context.Context, req domainSend.StickerRequest) (domainSend.GenericResponse, error) {
	if !req.IsScheduled() {
		return s.base.SendSticker(ctx, req)
	}
	return s.scheduler.Create(ctx, "sticker", req.Phone, "Sticker", req.ScheduleOptions, req, []scheduledAssetInput{{Field: "sticker", Header: req.Sticker}})
}
func (s *scheduledSendService) SendContact(ctx context.Context, req domainSend.ContactRequest) (domainSend.GenericResponse, error) {
	if !req.IsScheduled() {
		return s.base.SendContact(ctx, req)
	}
	return s.scheduler.Create(ctx, "contact", req.Phone, req.ContactName, req.ScheduleOptions, req, nil)
}
func (s *scheduledSendService) SendLink(ctx context.Context, req domainSend.LinkRequest) (domainSend.GenericResponse, error) {
	if !req.IsScheduled() {
		return s.base.SendLink(ctx, req)
	}
	return s.scheduler.Create(ctx, "link", req.Phone, req.Caption, req.ScheduleOptions, req, nil)
}
func (s *scheduledSendService) SendLocation(ctx context.Context, req domainSend.LocationRequest) (domainSend.GenericResponse, error) {
	if !req.IsScheduled() {
		return s.base.SendLocation(ctx, req)
	}
	return s.scheduler.Create(ctx, "location", req.Phone, "Location", req.ScheduleOptions, req, nil)
}
func (s *scheduledSendService) SendPoll(ctx context.Context, req domainSend.PollRequest) (domainSend.GenericResponse, error) {
	if !req.IsScheduled() {
		return s.base.SendPoll(ctx, req)
	}
	return s.scheduler.Create(ctx, "poll", req.Phone, req.Question, req.ScheduleOptions, req, nil)
}
func (s *scheduledSendService) SendPresence(ctx context.Context, req domainSend.PresenceRequest) (domainSend.GenericResponse, error) {
	return s.base.SendPresence(ctx, req)
}
func (s *scheduledSendService) SendChatPresence(ctx context.Context, req domainSend.ChatPresenceRequest) (domainSend.GenericResponse, error) {
	return s.base.SendChatPresence(ctx, req)
}
func (s *scheduledSendService) SendForward(ctx context.Context, req domainSend.ForwardRequest) (domainSend.GenericResponse, error) {
	if !req.IsScheduled() {
		return s.base.SendForward(ctx, req)
	}
	return s.scheduler.Create(ctx, "forward", req.Phone, "Forwarded message", req.ScheduleOptions, req, nil)
}

var _ domainSend.IScheduleUsecase = (*ScheduleService)(nil)
var _ domainSend.ISendUsecase = (*scheduledSendService)(nil)
