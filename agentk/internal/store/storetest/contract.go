// Package storetest 是存储层的共用契约：MySQL 与内存两个实现跑同一套断言，
// 语义分叉当场暴露。覆盖面即消费方接口之和——service.Store + pipeline.Store 的
// 全部方法（会话、事件日志）。
//
// 只有真库才能验的东西不在这里——自增 id 顺序、唯一键冲突、JSON 列
// 驱动往返，留在 internal/store 的 DSN 门控测试里。
package storetest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"agentk/internal/event"
	"agentk/internal/types"
)

// Store 是契约覆盖的读写面（= service.Store ∪ pipeline.Store，*store.Store 与
// *memory.Store 都满足）。未命中一律返回可被 errors.Is(err, types.ErrNotFound)
// 识别的错误。
type Store interface {
	CreateSession(ctx context.Context, tenantID, title string, profile []byte) (types.Session, error)
	GetSession(ctx context.Context, id int64) (types.Session, error)
	GetSessionByPublicID(ctx context.Context, tenantID, publicID string) (types.Session, error)
	ListSessions(ctx context.Context, tenantID string, beforeID int64, limit int) ([]types.Session, error)
	AppendEvents(ctx context.Context, sessionID int64, executionID types.ExecutionID, batchID event.BatchID, drafts []event.Draft) ([]event.Event, error)
	LoadSessionEvents(ctx context.Context, sessionID int64) ([]event.Event, error)
}

const testExecutionID types.ExecutionID = "exe_0191f38d-99f2-7d91-9f2b-712ab18e0a00"

// contractProfile 是契约用例创建会话时使用的固定 profile（存储层只验证它是 JSON object）。
const contractProfile = `{"system":"","tools":[]}`

// RunStoreContract 跑全部契约用例；newStore 每个用例被调一次，返回干净的实现。
func RunStoreContract(t *testing.T, newStore func(t *testing.T) Store) {
	t.Helper()
	cases := []struct {
		name string
		run  func(t *testing.T, s Store)
	}{
		{"CreateSessionRejectsBadProfile", testCreateSessionRejectsBadProfile},
		{"GetSessionByIDAndPublicID", testGetSessionByIDAndPublicID},
		{"SessionLookupIsTenantScoped", testSessionLookupIsTenantScoped},
		{"ListSessionsKeysetPaged", testListSessionsKeysetPaged},
		{"AppendAssignsEventIDsAndPreservesOrder", testAppendAssignsEventIDsAndPreservesOrder},
		{"AppendIsIdempotentPerBatch", testAppendIsIdempotentPerBatch},
		{"AppendRejectsEmptyBatch", testAppendRejectsEmptyBatch},
		{"AppendRejectsUnknownSession", testAppendRejectsUnknownSession},
		{"LoadUnknownSessionIsNotFound", testLoadUnknownSessionIsNotFound},
		{"PayloadsSurviveRoundTrip", testPayloadsSurviveRoundTrip},
		{"RoundIDIsOptional", testRoundIDIsOptional},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { tc.run(t, newStore(t)) })
	}
}

func newSession(t *testing.T, s Store, tenantID string) types.Session {
	t.Helper()
	sess, err := s.CreateSession(context.Background(), tenantID, "契约", []byte(contractProfile))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return sess
}

/* ---------- sessions ---------- */

func testCreateSessionRejectsBadProfile(t *testing.T, s Store) {
	for _, profile := range []string{"", "null", "[]", "{oops}"} {
		if _, err := s.CreateSession(context.Background(), "org_test", "t", []byte(profile)); err == nil {
			t.Errorf("profile %q was accepted, want rejection", profile)
		}
	}
}

func testGetSessionByIDAndPublicID(t *testing.T, s Store) {
	sess := newSession(t, s, "org_a")
	byID, err := s.GetSession(context.Background(), sess.ID)
	if err != nil || byID.PublicID != sess.PublicID || byID.TenantID != "org_a" {
		t.Fatalf("GetSession = %+v, %v", byID, err)
	}
	if string(byID.Profile) == "" {
		t.Fatal("profile must round-trip")
	}
	if _, err := s.GetSession(context.Background(), 999999); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("unknown session err = %v, want ErrNotFound", err)
	}
}

// testSessionLookupIsTenantScoped 钉住"不存在"与"不属于你"同一律：跨租户探测
// 不该能从错误码分辨资源是否存在。
func testSessionLookupIsTenantScoped(t *testing.T, s Store) {
	sess := newSession(t, s, "org_a")
	if _, err := s.GetSessionByPublicID(context.Background(), "org_a", sess.PublicID); err != nil {
		t.Fatalf("own session: %v", err)
	}
	if _, err := s.GetSessionByPublicID(context.Background(), "org_b", sess.PublicID); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("cross-tenant err = %v, want ErrNotFound", err)
	}
	// 空租户（历史遗留数据的取值）也不得命中
	if _, err := s.GetSessionByPublicID(context.Background(), "", sess.PublicID); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("empty-tenant err = %v, want ErrNotFound", err)
	}
	if _, err := s.GetSessionByPublicID(context.Background(), "org_a", "ses_missing"); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("unknown public id err = %v, want ErrNotFound", err)
	}
}

func testListSessionsKeysetPaged(t *testing.T, s Store) {
	var ids []int64
	for range 3 {
		ids = append(ids, newSession(t, s, "org_a").ID)
	}
	newSession(t, s, "org_b")

	page, err := s.ListSessions(context.Background(), "org_a", 0, 2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page) != 2 || page[0].ID != ids[2] || page[1].ID != ids[1] {
		t.Fatalf("page = %v, want newest first (%d, %d)", sessionIDs(page), ids[2], ids[1])
	}
	next, err := s.ListSessions(context.Background(), "org_a", page[1].ID, 2)
	if err != nil {
		t.Fatalf("list next: %v", err)
	}
	if len(next) != 1 || next[0].ID != ids[0] {
		t.Fatalf("next page = %v, want [%d]", sessionIDs(next), ids[0])
	}
	if _, err := s.ListSessions(context.Background(), "org_a", 0, 0); err == nil {
		t.Fatal("limit 0 accepted, want rejection")
	}
}

/* ---------- 事件日志 ---------- */

func testAppendAssignsEventIDsAndPreservesOrder(t *testing.T, s Store) {
	sessionID := newSession(t, s, "org_test").ID
	first, err := s.AppendEvents(context.Background(), sessionID, testExecutionID, "batch-1", []event.Draft{
		{Payload: event.UserInput{Text: "一"}},
		{RoundID: "rnd-1", Payload: event.RoundStarted{Model: "m", Dialect: event.DialectOpenAI}},
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if len(first) != 2 || first[0].ID == first[1].ID {
		t.Fatalf("event IDs = %v, want two unique IDs", eventIDs(first))
	}
	for _, ev := range first {
		parsed, err := uuid.Parse(strings.TrimPrefix(string(ev.ID), "evt_"))
		if err != nil || parsed.Version() != 7 || ev.ExecutionID != testExecutionID {
			t.Fatalf("event identity = %+v, parse err %v", ev, err)
		}
	}
	second, err := s.AppendEvents(context.Background(), sessionID, testExecutionID, "batch-2", []event.Draft{
		{RoundID: "rnd-1", Payload: event.AssistantText{Text: "二"}},
	})
	if err != nil {
		t.Fatalf("append second batch: %v", err)
	}
	if second[0].CreatedAt.IsZero() {
		t.Fatal("createdAt is zero, event log must timestamp appended events")
	}
}

// testAppendIsIdempotentPerBatch 钉住 Activity 重试语义：同一 batch id 重放不得
// 写出第二份历史，且必须返回首次提交的那批。
func testAppendIsIdempotentPerBatch(t *testing.T, s Store) {
	sessionID := newSession(t, s, "org_test").ID
	drafts := []event.Draft{
		{RoundID: "rnd-1", Payload: event.AssistantText{Text: "首次"}},
		{RoundID: "rnd-1", Payload: event.RoundCompleted{ModelMillis: 12}},
	}
	first, err := s.AppendEvents(context.Background(), sessionID, testExecutionID, "round-1", drafts)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	// 重试常常带着不同的模型输出回来（采样是随机的）；批已提交则一律以首次为准。
	retry, err := s.AppendEvents(context.Background(), sessionID, testExecutionID, "round-1", []event.Draft{
		{RoundID: "rnd-1", Payload: event.AssistantText{Text: "重试产出了别的东西"}},
	})
	if err != nil {
		t.Fatalf("retry append: %v", err)
	}
	if len(retry) != len(first) {
		t.Fatalf("retry returned %d events, want the %d already committed", len(retry), len(first))
	}
	if eventIDs(retry)[0] != eventIDs(first)[0] || eventIDs(retry)[1] != eventIDs(first)[1] {
		t.Fatalf("retry IDs = %v, want %v", eventIDs(retry), eventIDs(first))
	}
	text, ok := retry[0].Payload.(event.AssistantText)
	if !ok || text.Text != "首次" {
		t.Fatalf("retry payload = %#v, want the first commit", retry[0].Payload)
	}
	all, err := s.LoadSessionEvents(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("event log holds %d events, want 2 (retry must not duplicate)", len(all))
	}
}

func testAppendRejectsEmptyBatch(t *testing.T, s Store) {
	sessionID := newSession(t, s, "org_test").ID
	if _, err := s.AppendEvents(context.Background(), sessionID, testExecutionID, "b", nil); err == nil {
		t.Fatal("expected empty batch to be rejected")
	}
	if _, err := s.AppendEvents(context.Background(), sessionID, testExecutionID, "", []event.Draft{
		{Payload: event.UserInput{Text: "x"}},
	}); err == nil {
		t.Fatal("expected missing batch id to be rejected")
	}
	if _, err := s.AppendEvents(context.Background(), sessionID, testExecutionID, "b", []event.Draft{{}}); err == nil {
		t.Fatal("expected nil payload to be rejected")
	}
}

func testAppendRejectsUnknownSession(t *testing.T, s Store) {
	_, err := s.AppendEvents(context.Background(), 999999, testExecutionID, "b", []event.Draft{
		{Payload: event.UserInput{Text: "x"}},
	})
	if !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// testLoadUnknownSessionIsNotFound 钉住空结果的歧义消解："会话存在但没事件"是
// 空切片，"会话不存在"必须 ErrNotFound——两个实现曾在这里分叉。
func testLoadUnknownSessionIsNotFound(t *testing.T, s Store) {
	if _, err := s.LoadSessionEvents(context.Background(), 999999); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	sessionID := newSession(t, s, "org_test").ID
	got, err := s.LoadSessionEvents(context.Background(), sessionID)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty session load = %v, %v; want empty, nil", eventIDs(got), err)
	}
}

// testPayloadsSurviveRoundTrip 每个 kind 落一遍库再读回来：事件日志存的是唯一真相，
// 任何一个 kind 在存储层丢字段都会让 fold 读到残缺历史。
func testPayloadsSurviveRoundTrip(t *testing.T, s Store) {
	assertSamplesCoverAllKinds(t)
	sessionID := newSession(t, s, "org_test").ID
	drafts := make([]event.Draft, 0, len(samplePayloads))
	for _, p := range samplePayloads {
		drafts = append(drafts, event.Draft{RoundID: "rnd-1", Payload: p})
	}
	if _, err := s.AppendEvents(context.Background(), sessionID, testExecutionID, "b1", drafts); err != nil {
		t.Fatalf("append: %v", err)
	}
	got, err := s.LoadSessionEvents(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != len(samplePayloads) {
		t.Fatalf("loaded %d events, want %d", len(got), len(samplePayloads))
	}
	for i, want := range samplePayloads {
		if got[i].Payload.Kind() != want.Kind() {
			t.Errorf("event %d kind = %q, want %q", i, got[i].Payload.Kind(), want.Kind())
			continue
		}
		assertSamePayload(t, i, want, got[i].Payload)
	}
}

func testRoundIDIsOptional(t *testing.T, s Store) {
	sessionID := newSession(t, s, "org_test").ID
	got, err := s.AppendEvents(context.Background(), sessionID, testExecutionID, "b1", []event.Draft{
		{Payload: event.UserInput{Text: "会话级事实不属于任何轮次"}},
		{RoundID: "rnd-1", Payload: event.RoundStarted{Model: "m", Dialect: event.DialectOpenAI}},
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if got[0].RoundID != "" {
		t.Errorf("roundId = %q, want empty", got[0].RoundID)
	}
	if got[1].RoundID != "rnd-1" {
		t.Errorf("roundId = %q, want rnd-1", got[1].RoundID)
	}
}

/* ---------- helpers ---------- */

func eventIDs(events []event.Event) []event.EventID {
	out := make([]event.EventID, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.ID)
	}
	return out
}

func sessionIDs(sessions []types.Session) []int64 {
	out := make([]int64, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, s.ID)
	}
	return out
}
