package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/storage"
)

// ReadList returns the text of a tracker's shared list; ok is false when the owner has none yet.
func (w *TrackerLists) ReadList(ctx context.Context, userID uuid.UUID, trackerType string) (text string, ok bool, err error) {
	if w == nil || w.Store == nil {
		return "", false, nil
	}
	list := w.list(ctx, userID, storage.ListKind, trackerType)
	if list == nil {
		return "", false, nil
	}
	text, err = w.Store.ReadText(ctx, userID, list.ID, trackerReadLimit)
	return text, err == nil, err
}

// WriteList replaces a tracker's shared list with text (the chat agent's row edits and their undo).
func (w *TrackerLists) WriteList(ctx context.Context, userID uuid.UUID, trackerType, text string) error {
	return w.replaceTrackerList(ctx, userID, trackerType, domain.TrackerTypes[trackerType]+" tracker", text)
}
