package browser

import (
	"context"
	"encoding/json"
	"github.com/chromedp/chromedp"
)

// Small helpers keep operation-scoped cancellation and JSON decoding inside
// this driver. The application layer never depends on the CDP library API.
type cdpActionFunc func(context.Context) error

func native(action chromedp.Action[chromedp.Void]) cdpActionFunc {
	return func(ctx context.Context) error { return chromedp.Do(ctx, action) }
}
func cdpNavigate(url string) cdpActionFunc       { return native(chromedp.Navigate(url)) }
func cdpReload() cdpActionFunc                   { return native(chromedp.Reload()) }
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
		var err error
		*out, err = chromedp.Run(ctx, chromedp.CaptureScreenshot())
		return err
	}
}
