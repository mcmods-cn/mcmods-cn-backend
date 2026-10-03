package httpapi

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"sync"
	"testing"
	"time"
)

func TestOCT02CExportPNGRequiresCompletePNGImage(t *testing.T) {
	var encodedPNG, encodedJPEG bytes.Buffer
	fixture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if err := png.Encode(&encodedPNG, fixture); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&encodedJPEG, fixture, nil); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		raw   []byte
		valid bool
	}{
		{"complete PNG", encodedPNG.Bytes(), true},
		{"PNG header without image data", encodedPNG.Bytes()[:33], false},
		{"JPEG renamed PNG", encodedJPEG.Bytes(), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			media, err := inspectExportPNG(context.Background(), defaultOSSConfig(), "owned-revision", "owned-project", "icons/fixture.png", test.raw, nil)
			if (err == nil) != test.valid {
				t.Fatalf("PNG validity=%t error=%v", test.valid, err)
			}
			if test.valid && (media.Width != 2 || media.Height != 2 || media.ByteLength != int64(len(test.raw))) {
				t.Fatalf("valid PNG metadata changed: %+v", media)
			}
		})
	}
}

func TestOCT02CExportPNGDecodeSlotHonorsCancellation(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	exportPNGDecodeSlots <- struct{}{}
	defer func() { <-exportPNGDecodeSlots }()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	waiting := &observedExportPNGContext{Context: ctx, observed: make(chan struct{})}
	cause := errors.New("owned import cancelled while waiting for decode")
	result := make(chan error, 1)
	go func() {
		_, err := inspectExportPNG(waiting, defaultOSSConfig(), "owned-revision", "owned-project", "icons/fixture.png", encoded.Bytes(), nil)
		result <- err
	}()
	select {
	case <-waiting.observed:
	case <-time.After(time.Second):
		t.Fatal("image did not reach occupied decode slot")
	}
	cancel(cause)
	select {
	case err := <-result:
		if !errors.Is(err, cause) {
			t.Fatalf("decode slot cancellation error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled image remained blocked on decode slot")
	}
}

type observedExportPNGContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (ctx *observedExportPNGContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.observed) })
	return ctx.Context.Done()
}
