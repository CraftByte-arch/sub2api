package userusageanalytics

import (
	"context"
	"testing"
)

func TestMark(t *testing.T) {
	if IsMarked(context.Background()) {
		t.Fatal("unmarked context must not enable user usage analytics")
	}
	if !IsMarked(Mark(context.Background())) {
		t.Fatal("marked context must enable user usage analytics")
	}
	if IsMarked(nil) {
		t.Fatal("nil context must not enable user usage analytics")
	}
}
