package provider

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
)

// A nil failure means an explicit false status or a legacy endpoint that does
// not implement GET (404/405). Failed reads must never authorize a POST.
func readCheckInStatus(ctx context.Context, session port.Session, site domain.Site) (bool, *domain.Result) {
	path, err := url.Parse(site.CheckInPath)
	if err != nil {
		r := failure(err)
		return false, &r
	}
	start, _ := domain.DayBounds(time.Now())
	query := path.Query()
	query.Set("month", start.Format("2006-01"))
	path.RawQuery = query.Encode()
	response, err := session.Request(ctx, http.MethodGet, path.String(), nil)
	var problem *domain.Result
	if err != nil {
		r := failure(err)
		problem = &r
	} else if problem = HTTPFailure(response); problem != nil {
		if (response.Status == http.StatusNotFound || response.Status == http.StatusMethodNotAllowed) && problem.Evidence != "challenge_required" {
			return false, nil
		}
	} else {
		payload, decodeErr := Decode(response.Body)
		if decodeErr == nil && payload["success"] == true {
			data := dataObject(payload)
			if checked, ok := data["checked_in"].(bool); ok {
				return checked, nil
			}
			stats, _ := data["stats"].(map[string]any)
			if checked, ok := stats["checked_in_today"].(bool); ok {
				return checked, nil
			}
		} else if decodeErr == nil && payload["success"] == false {
			r := Normalize("newapi", response)
			if r.Complete() {
				return true, nil
			}
			problem = &r
		}
		if problem == nil {
			problem = &domain.Result{Status: domain.NeedsAttention, Message: "签到状态响应无效或缺少今日状态，未提交签到", Evidence: "invalid_status", HTTPStatus: response.Status, Advice: "检查站点状态接口；不支持接口签到的站点可配置网页按钮签到。"}
		}
	}
	problem.Stage = "status_check"
	return false, problem
}

func confirmedCheckIn(ctx context.Context, session port.Session, site domain.Site) bool {
	done, problem := readCheckInStatus(ctx, session, site)
	return done && problem == nil
}
