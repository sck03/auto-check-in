package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// Small helpers keep operation-scoped cancellation and JSON decoding inside
// this driver. The application layer never depends on the CDP library API.
type cdpActionFunc func(context.Context) error

func enableNetwork(ctx context.Context) error {
	// Keep small login responses across renderer swaps on OAuth redirects.
	// Bound the buffers so unrelated large assets cannot grow them indefinitely.
	total, resource, durable := int64(2<<20), int64(1<<20), true
	_, err := cdp.Call(ctx, chromedp.FromContext(ctx).Target, network.Enable, network.EnableParams{
		MaxTotalBufferSize: &total, MaxResourceBufferSize: &resource, EnableDurableMessages: &durable,
	})
	return err
}

func native(action chromedp.Action[chromedp.Void]) cdpActionFunc {
	return func(ctx context.Context) error { return chromedp.Do(ctx, action) }
}
func cdpReload() cdpActionFunc {
	return func(ctx context.Context) error {
		_, err := cdp.Call(ctx, chromedp.FromContext(ctx).Target, page.Reload, page.ReloadParams{})
		return err
	}
}
func cdpNavigateAsync(url string) cdpActionFunc {
	return func(ctx context.Context) error {
		result, err := cdp.Call(ctx, chromedp.FromContext(ctx).Target, page.Navigate, page.NavigateParams{URL: url})
		if err == nil && result.ErrorText != "" {
			return fmt.Errorf("navigation failed: %s", result.ErrorText)
		}
		return err
	}
}
func cdpNavigateDOMReady(url string) cdpActionFunc {
	return func(ctx context.Context) error {
		_, err := chromedp.Run(ctx, chromedp.WaitEvent(page.DomContentEventFired, nil,
			chromedp.Func(func(ctx context.Context, _ *chromedp.Target) error {
				return cdpNavigateAsync(url)(ctx)
			})))
		return err
	}
}
func cdpMouseClickXY(x, y float64) cdpActionFunc { return native(chromedp.MouseClickXY(x, y)) }
func cdpKeyEvent(key string, opts ...chromedp.KeyOption) cdpActionFunc {
	return native(chromedp.KeyEvent(key, opts...))
}
func cdpEvaluate(expr string, out any, opts ...chromedp.EvaluateOption) cdpActionFunc {
	return func(ctx context.Context) error {
		if out == nil {
			return chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](expr, opts...))
		}
		value, err := chromedp.Run(ctx, chromedp.Evaluate[[]byte](expr, opts...))
		if err != nil {
			return err
		}
		return json.Unmarshal(value, out)
	}
}
func cdpPoll(expr string, out any, opts ...chromedp.PollOption) cdpActionFunc {
	return func(ctx context.Context) error {
		if out == nil {
			return chromedp.Do(ctx, chromedp.Poll[chromedp.Void](expr, opts...))
		}
		value, err := chromedp.Run(ctx, chromedp.Poll[[]byte](expr, opts...))
		if err != nil {
			return err
		}
		return json.Unmarshal(value, out)
	}
}
func cdpCaptureScreenshot(out *[]byte) cdpActionFunc {
	return func(ctx context.Context) error {
		quality, yes, no := int64(80), true, false
		result, err := cdp.Call(ctx, chromedp.FromContext(ctx).Target, page.CaptureScreenshot, page.CaptureScreenshotParams{
			Format: page.CaptureScreenshotFormatJpeg, Quality: &quality,
			FromSurface: &yes, CaptureBeyondViewport: &no, OptimizeForSpeed: &yes,
		})
		*out = result.Data
		return err
	}
}
