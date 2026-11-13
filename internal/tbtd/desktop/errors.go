// Copyright (C) 2026 crawter
// All rights reserved.

// Package desktop contains desktop presentation boundary contracts.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sync"

	monkit "github.com/spacemonkeygo/monkit/v3"

	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/session"
)

var mon = monkit.Package()

var diagnosticIDPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)+$`)

// ErrorPresentation contains only safe player-facing information. Choices are
// semantic actions, not permission to bypass the session's recovery policy.
type ErrorPresentation struct {
	Code    session.ErrorCode
	Message string
	Choices []string
}

// PresentError classifies by identity, never by an underlying error's text.
// A nil error produces no presentation. Storage choices must still be limited
// to the choices actually supplied by the session.
func PresentError(err error) ErrorPresentation {
	if err == nil {
		return ErrorPresentation{}
	}
	code := session.Internal
	for _, candidate := range []session.ErrorCode{session.InvalidAction, session.ContentInvalid, session.StorageDenied, session.StorageFull, session.StorageConflict, session.SaveCorrupt, session.SaveTooNew, session.ContentIncompatible, session.GraphicsUnavailable, session.AudioUnavailable, session.Internal} {
		if errors.Is(err, candidate) {
			code = candidate
			break
		}
	}
	if code == session.Internal && (content.ErrInvalid.Has(err) || errors.Is(err, content.ErrMissingReference) || content.Error.Has(err)) {
		code = session.ContentInvalid
	}
	result := ErrorPresentation{Code: code}
	switch code {
	case session.InvalidAction:
		result.Message = "Действие сейчас недоступно."
	case session.ContentInvalid:
		result.Message, result.Choices = "Не удалось загрузить необходимые материалы игры. Проверьте файлы игры.", []string{"retry", "exit"}
	case session.StorageDenied:
		result.Message, result.Choices = "Нет доступа к сохранению или профиль занят.", []string{"retry_save", "reload_saved", "exit_unsaved"}
	case session.StorageFull:
		result.Message, result.Choices = "Недостаточно места для сохранения.", []string{"retry_save", "reload_saved", "exit_unsaved"}
	case session.StorageConflict:
		result.Message, result.Choices = "Сохранение изменилось. Необходимо заново загрузить прогресс.", []string{"reload_saved", "exit_unsaved"}
	case session.SaveCorrupt:
		result.Message, result.Choices = "Не удалось прочитать сохранение. Повреждённые файлы сохранены.", []string{"choose_older", "new_game", "exit"}
	case session.SaveTooNew, session.ContentIncompatible:
		result.Message, result.Choices = "Сохранение несовместимо с этой версией игры. Файлы сохранены.", []string{"new_game", "exit"}
	case session.GraphicsUnavailable:
		result.Message, result.Choices = "Графическое устройство недоступно. Прогресс сохранён в последней подтверждённой версии.", []string{"exit"}
	case session.AudioUnavailable:
		result.Message, result.Choices = "Звук недоступен. Можно продолжить без звука или повторить попытку.", []string{"continue_silent", "retry_audio"}
	default:
		result.Message, result.Choices = "Произошла внутренняя ошибка. Загрузите сохранённый прогресс или выйдите.", []string{"reload_saved", "exit_unsaved"}
	}
	return result
}

// PresentContentError attaches a separately supplied inventory identity, never
// a path or an error string. The caller supplies its validated manifest.
func PresentContentError(ctx context.Context, cause error, manifest *content.Manifest, id string) (_ ErrorPresentation, err error) {
	defer mon.Task()(&ctx)(&err)
	result := PresentError(cause)
	if result.Code != session.ContentInvalid || manifest == nil || len(id) > 160 || !diagnosticIDPattern.MatchString(id) {
		return ErrorPresentation{}, fmt.Errorf("invalid content diagnostic")
	}
	found := false
	for _, record := range manifest.Records {
		if record.ID == id {
			found = true
			break
		}
	}
	if !found {
		for _, identity := range manifest.Campaign {
			if identity.ID == id {
				found = true
				break
			}
		}
	}
	if !found {
		return ErrorPresentation{}, fmt.Errorf("unknown content diagnostic identity")
	}
	result.Message += " Идентификатор: " + id
	return result, nil
}

// Failure is one owned failure occurrence; it may be presented repeatedly but
// only the first handling call emits a diagnostic. It must not be copied.
type Failure struct {
	mu       sync.Mutex
	reported bool
}

// ErrorHandler records failures through an injected process-owned logger.
type ErrorHandler struct {
	log *slog.Logger
}

// NewErrorHandler requires a logger supplied by the composition root.
func NewErrorHandler(log *slog.Logger) (*ErrorHandler, error) {
	if log == nil {
		return nil, errors.New("missing error logger")
	}
	return &ErrorHandler{log: log}, nil
}

// Handle records the error class once for this occurrence. It deliberately
// excludes underlying errors, paths and player data from the diagnostic.
func (handler *ErrorHandler) Handle(ctx context.Context, occurrence *Failure, cause error) (_ ErrorPresentation, err error) {
	defer mon.Task()(&ctx)(&err)
	if handler == nil || handler.log == nil || occurrence == nil || cause == nil {
		return ErrorPresentation{}, errors.New("missing error handling dependency")
	}
	result := PresentError(cause)
	occurrence.mu.Lock()
	defer occurrence.mu.Unlock()
	if !occurrence.reported && result.Code != session.InvalidAction {
		handler.log.ErrorContext(ctx, "desktop failure", "class", string(result.Code))
		occurrence.reported = true
	}
	return result, nil
}
