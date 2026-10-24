package app

import (
	"context"
	"testing"
	"testing/synctest"

	"auto-check-in/internal/domain"
	"auto-check-in/internal/port"
)

type settlingLoginSession struct {
	port.InteractiveSession
	reads     int
	snapshots int
	wrong     bool
}

func (*settlingLoginSession) ConfirmedLogin(context.Context) bool { return false }
func (s *settlingLoginSession) Snapshot(context.Context) (domain.Credentials, error) {
	s.snapshots++
	return domain.Credentials{Method: "cookie", APIUser: "1", CookieHeader: "session=fixture"}, nil
}
func (s *settlingLoginSession) Request(context.Context, string, string, any) (port.Response, error) {
	s.reads++
	if s.wrong {
		return port.Response{Status: 200, Body: []byte(`{"success":true,"data":{"id":2}}`)}, nil
	}
	if s.reads < 3 {
		return port.Response{Status: 401}, nil
	}
	return port.Response{Status: 200, Body: []byte(`{"success":true,"data":{"id":1}}`)}, nil
}

func TestSaveLoginWaitsOnlyForReadOnlySessionSettlement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		session := &settlingLoginSession{}
		entry := &loginSession{session: session, site: domain.Templates()[2].Site, expectedUser: "1"}
		c, err := verifyLoginSession(context.Background(), entry)
		if err != nil || c.APIUser != "1" || session.reads != 3 || session.snapshots < 3 {
			t.Fatal("login did not settle", err)
		}
	})
}

func TestSaveLoginDoesNotRetryOrAcceptWrongIdentity(t *testing.T) {
	session := &settlingLoginSession{wrong: true}
	entry := &loginSession{session: session, site: domain.Templates()[2].Site, expectedUser: "1"}
	if _, err := verifyLoginSession(context.Background(), entry); err == nil || session.reads != 1 {
		t.Fatal("wrong identity was accepted or retried")
	}
}
