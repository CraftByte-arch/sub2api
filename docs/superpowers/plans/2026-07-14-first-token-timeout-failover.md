---
change: configurable-first-token-timeout-failover
design-doc: docs/superpowers/specs/2026-07-14-first-token-timeout-failover-design.md
base-ref: e15265205a50addfeba66f935b7e256ea2a51f20
---

# 可配置首 Token 超时、换号与统计 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为三个 HTTP 文本流协议增加可热更新的首 token 超时与安全换号，并在独立管理员页面准确展示 attempt/request 失败率和每账号超时率。

**Architecture:** handler 为每个账号 attempt 安装独立 controller 与事务化 response gate，协议输出点通过纯 detector 报告首个语义增量；失败继续复用现有 `UpstreamFailoverError` 循环。统计通过独立有界 recorder 汇总到一张 PostgreSQL 小时表，查询 API 与 `/admin/ttft` 页面不并入现有大型 Ops/Settings 模块。

**Tech Stack:** Go 1.x、Gin、Ent settings、`database/sql`、PostgreSQL、Redis pub/sub、Wire、Vue 3、TypeScript、Pinia/Vue Router、Chart.js、Vitest。

---

## 文件结构

新增文件坚持单一职责：

- `backend/internal/service/first_token_timeout_settings.go`：设置、原子策略快照、热更新。
- `backend/internal/service/first_token_timeout_attempt.go`：attempt 状态机、context、timer。
- `backend/internal/handler/first_token_response_gate.go`：`gin.ResponseWriter` 事务门。
- `backend/internal/service/first_token_detector.go`：三协议纯 detector。
- `backend/internal/service/first_token_timeout_stats.go`：统计事件、outcome、failure kind、repository port。
- `backend/internal/service/first_token_timeout_tracking.go`：request/attempt 恰好一次终结。
- `backend/internal/service/first_token_timeout_stats_recorder.go`：异步聚合、flush、健康和清理。
- `backend/internal/repository/first_token_timeout_stats_repo.go`：SQL UPSERT 与查询。
- `backend/internal/handler/admin/first_token_timeout_handler.go`：独立管理 API。
- `frontend/src/api/admin/ttft.ts`：页面专用类型与 API。
- `frontend/src/views/admin/ttft/`：页面及局部图表/表格组件。

现有大型文件只做追加式接入：gateway handler/failover loop、目标流写出点、Wire provider、admin routes、前端 router/sidebar/locale index。

### Task 1: 设置、策略快照与热更新（OpenSpec 1.1、1.3）

**Files:**
- Create: `backend/internal/service/first_token_timeout_settings.go`
- Create: `backend/internal/service/first_token_timeout_settings_test.go`
- Create: `backend/internal/repository/first_token_timeout_policy_notifier.go`
- Create: `backend/internal/repository/first_token_timeout_policy_notifier_test.go`
- Modify: `backend/internal/service/domain_constants.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `backend/internal/repository/wire.go`
- Modify: `backend/cmd/server/wire_gen.go`

- [x] **Step 1: 写设置解析和策略并发读取的失败测试**

```go
func TestParseFirstTokenTimeoutSettingsFallbackAndValidation(t *testing.T) {
    require.Equal(t, FirstTokenTimeoutSettings{Enabled: false, TimeoutSeconds: 30},
        parseFirstTokenTimeoutSettings(`{"enabled":true,"timeout_seconds":0}`))
    require.Error(t, validateFirstTokenTimeoutSettings(FirstTokenTimeoutSettings{Enabled: true, TimeoutSeconds: 301}))
}

func TestFirstTokenTimeoutPolicyUpdatePublishesImmutableSnapshot(t *testing.T) {
    policy := newFirstTokenTimeoutPolicyForTest(t)
    require.NoError(t, policy.Update(context.Background(), FirstTokenTimeoutSettings{Enabled: true, TimeoutSeconds: 12}))
    snap := policy.Snapshot()
    require.True(t, snap.Enabled)
    require.Equal(t, 12*time.Second, snap.Timeout)
}
```

- [x] **Step 2: 运行测试并确认失败**

Run: `cd backend && go test ./internal/service -run 'Test(ParseFirstTokenTimeoutSettings|FirstTokenTimeoutPolicy)' -count=1`

Expected: FAIL，提示 `FirstTokenTimeoutSettings` 或 `FirstTokenTimeoutPolicy` 未定义。

- [x] **Step 3: 实现设置与原子策略最小接口**

```go
const SettingKeyFirstTokenTimeoutSettings = "first_token_timeout_settings"

type FirstTokenTimeoutSettings struct {
    Enabled        bool `json:"enabled"`
    TimeoutSeconds int  `json:"timeout_seconds"`
}

type FirstTokenTimeoutSnapshot struct {
    Enabled  bool
    Timeout  time.Duration
    LoadedAt time.Time
}

type FirstTokenTimeoutPolicy struct {
    repo     SettingRepository
    notifier FirstTokenTimeoutPolicyNotifier
    current atomic.Value // FirstTokenTimeoutSnapshot
}

func (p *FirstTokenTimeoutPolicy) Snapshot() FirstTokenTimeoutSnapshot
func (p *FirstTokenTimeoutPolicy) Update(ctx context.Context, in FirstTokenTimeoutSettings) error
func (p *FirstTokenTimeoutPolicy) Reload(ctx context.Context) error
func (p *FirstTokenTimeoutPolicy) Start(ctx context.Context)
```

默认关闭且默认阈值 30 秒；Update 先校验并持久化，再替换本实例快照并发布 invalidation。notifier 使用现有 Redis client 的独立 channel；无 Redis 时 Start 用低频 DB reload ticker 兜底。所有 goroutine 由传入 context 停止。

- [x] **Step 4: 覆盖跨实例 invalidation 与损坏配置回退**

在 notifier 测试中用项目 Redis 测试替身验证 publish/subscribe；在 service 测试中让 repo 返回损坏 JSON，断言 snapshot 回退关闭而 Update 非法值不覆盖旧 snapshot。

- [x] **Step 5: 运行目标测试和 race 测试**

Run: `cd backend && go test -race ./internal/service ./internal/repository -run 'FirstTokenTimeout' -count=1`

Expected: PASS，无 data race。

- [x] **Step 6: 生成 Wire 并提交**

Run: `cd backend && go generate ./cmd/server`

Expected: `backend/cmd/server/wire_gen.go` 成功更新，编译无缺失 provider。

```bash
git add backend/internal/service/first_token_timeout_settings.go backend/internal/service/first_token_timeout_settings_test.go backend/internal/service/domain_constants.go backend/internal/service/wire.go backend/internal/repository/first_token_timeout_policy_notifier.go backend/internal/repository/first_token_timeout_policy_notifier_test.go backend/internal/repository/wire.go backend/cmd/server/wire_gen.go
git commit -m "feat: add first token timeout policy"
```

### Task 2: Attempt controller 与事务化 response gate（OpenSpec 2.1-2.4）

**Files:**
- Create: `backend/internal/service/first_token_timeout_attempt.go`
- Create: `backend/internal/service/first_token_timeout_attempt_test.go`
- Create: `backend/internal/handler/first_token_response_gate.go`
- Create: `backend/internal/handler/first_token_response_gate_test.go`

- [x] **Step 1: 写状态机竞态与 timer 释放失败测试**

```go
func TestFirstTokenAttemptCommitAndTimeoutHaveSingleWinner(t *testing.T) {
    for i := 0; i < 500; i++ {
        a := NewFirstTokenAttempt(context.Background(), time.Nanosecond)
        var winners atomic.Int32
        var wg sync.WaitGroup
        wg.Add(2)
        go func() { defer wg.Done(); if a.MarkFirstToken() { winners.Add(1) } }()
        go func() { defer wg.Done(); if a.TimeoutForTest() { winners.Add(1) } }()
        wg.Wait()
        require.Equal(t, int32(1), winners.Load())
        a.Close()
    }
}
```

- [x] **Step 2: 运行 controller 测试确认失败**

Run: `cd backend && go test ./internal/service -run 'FirstTokenAttempt' -count=1`

Expected: FAIL，提示 controller 构造器未定义。

- [x] **Step 3: 实现 controller 最小状态机**

```go
type FirstTokenAttemptState uint32
const (
    FirstTokenPending FirstTokenAttemptState = iota
    FirstTokenCommitted
    FirstTokenTimedOut
    FirstTokenCanceled
)

var ErrFirstTokenTimeout = errors.New("first token timeout")
var ErrFirstTokenPreludeTooLarge = errors.New("first token prelude too large")

type FirstTokenAttempt struct {
    ctx context.Context
    cancel context.CancelCauseFunc
    state atomic.Uint32
    timer *time.Timer
    startedAt time.Time
}
```

实现 `Context`、`MarkFirstToken`、`Cancel`、`State`、`Elapsed`、`Close`；所有终态 CAS 后停止 timer，父 context 取消映射为 canceled。

- [x] **Step 4: 写 gate header/body/Flush/overflow 失败测试**

```go
func TestFirstTokenResponseGateRollbackLeaksNothing(t *testing.T) {
    base := newGinTestWriter()
    gate := NewFirstTokenResponseGate(base, attempt, 256<<10)
    gate.Header().Set("X-Upstream-Request-ID", "failed")
    gate.WriteHeader(http.StatusOK)
    _, _ = gate.WriteString("event: ping\n\n")
    gate.Flush()
    gate.Rollback()
    require.False(t, base.Written())
    require.Empty(t, base.Header().Get("X-Upstream-Request-ID"))
    require.Empty(t, base.Body.String())
}
```

- [x] **Step 5: 运行 gate 测试确认失败**

Run: `cd backend && go test ./internal/handler -run 'FirstTokenResponseGate' -count=1`

Expected: FAIL，提示 gate 未定义。

- [x] **Step 6: 实现完整 writer 契约与 256 KiB 上限**

实现 `Header`、`Write`、`WriteString`、`WriteHeader`、`Status`、`Size`、`Written`、`WriteHeaderNow`、`Flush`、`CloseNotify`、`Hijack`、`Pusher`、`Commit`、`Rollback`。pending 下只写本地副本；Commit 按 header/status/body 顺序写出并切直写；溢出 cancel cause 为 `ErrFirstTokenPreludeTooLarge`。

- [x] **Step 7: 运行 race 与接口测试**

Run: `cd backend && go test -race ./internal/service ./internal/handler -run 'FirstToken(Attempt|ResponseGate)' -count=1`

Expected: PASS，commit/timeout 竞态中无双写、无 race。

- [x] **Step 8: 提交**

```bash
git add backend/internal/service/first_token_timeout_attempt.go backend/internal/service/first_token_timeout_attempt_test.go backend/internal/handler/first_token_response_gate.go backend/internal/handler/first_token_response_gate_test.go
git commit -m "feat: gate streaming output until first token"
```

### Task 3: 三协议语义 detector（OpenSpec 3.1）

**Files:**
- Create: `backend/internal/service/first_token_detector.go`
- Create: `backend/internal/service/first_token_detector_test.go`
- Create: `backend/internal/service/testdata/first_token_events/` fixtures

- [x] **Step 1: 用真实 SSE data 写表驱动失败测试**

```go
tests := []struct{ protocol FirstTokenProtocol; event, data string; want bool }{
    {ProtocolResponses, "", `{"type":"response.created"}`, false},
    {ProtocolResponses, "", `{"type":"response.output_text.delta","delta":"hi"}`, true},
    {ProtocolChatCompletions, "", `{"choices":[{"delta":{"role":"assistant"}}]}`, false},
    {ProtocolChatCompletions, "", `{"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"{\\\"x\\\":"}}]}}]}`, true},
    {ProtocolAnthropicMessages, "ping", `{}`, false},
    {ProtocolAnthropicMessages, "content_block_delta", `{"delta":{"type":"thinking_delta","thinking":"x"}}`, true},
}
```

- [x] **Step 2: 运行并确认失败**

Run: `cd backend && go test ./internal/service -run 'FirstTokenDetector' -count=1`

Expected: FAIL，提示 detector 类型或函数未定义。

- [x] **Step 3: 实现无副作用 detector**

```go
func IsFirstSemanticToken(protocol FirstTokenProtocol, eventName string, data []byte) bool {
    switch protocol {
    case ProtocolResponses: return isResponsesSemanticDelta(data)
    case ProtocolChatCompletions: return isChatSemanticDelta(data)
    case ProtocolAnthropicMessages: return isAnthropicSemanticDelta(eventName, data)
    default: return false
    }
}
```

仅检查非空 payload，不 `TrimSpace` 删除真实空白输出；字段缺失、空字符串、role-only、usage、ping、生命周期和终止事件返回 false。

- [x] **Step 4: 补齐 reasoning/summary/function/tool/input-json/空 delta fixtures 并运行测试**

Run: `cd backend && go test ./internal/service -run 'FirstTokenDetector' -count=1`

Expected: PASS，三个协议所有正反例通过。

- [x] **Step 5: 提交**

```bash
git add backend/internal/service/first_token_detector.go backend/internal/service/first_token_detector_test.go backend/internal/service/testdata/first_token_events
git commit -m "feat: detect semantic first token events"
```

### Task 4: 将 controller/gate 接入全部目标 HTTP pipeline（OpenSpec 3.2-3.5、4.2）

**Files:**
- Create: `backend/internal/handler/first_token_attempt_runner.go`
- Create: `backend/internal/handler/first_token_attempt_runner_test.go`
- Modify: `backend/internal/handler/gateway_handler_responses.go`
- Modify: `backend/internal/handler/gateway_handler_chat_completions.go`
- Modify: `backend/internal/handler/gateway_handler.go`
- Modify: `backend/internal/handler/openai_gateway_handler.go`
- Modify: `backend/internal/handler/openai_chat_completions.go`
- Modify: `backend/internal/service/gateway_forward_as_responses.go`
- Modify: `backend/internal/service/gateway_forward_as_chat_completions.go`
- Modify: `backend/internal/service/gateway_anthropic_passthrough.go`
- Modify: `backend/internal/service/gemini_chat_completions_compat_service.go`
- Modify: `backend/internal/service/gemini_messages_compat_service.go`
- Modify: `backend/internal/service/antigravity_gateway_streaming.go`
- Modify: `backend/internal/service/bedrock_stream.go`
- Modify: `backend/internal/service/openai_gateway_forward.go`
- Modify: `backend/internal/service/openai_gateway_passthrough.go`
- Modify: `backend/internal/service/openai_gateway_response_handling.go`
- Modify: `backend/internal/service/openai_gateway_cc_pipeline.go`
- Modify: `backend/internal/service/openai_gateway_chat_completions.go`
- Modify: `backend/internal/service/openai_gateway_chat_completions_raw.go`
- Modify: `backend/internal/service/openai_gateway_messages.go`
- Modify: `backend/internal/service/openai_gateway_messages_chat_fallback.go`
- Modify: `backend/internal/service/openai_gateway_responses_chat_fallback.go`
- Test: `backend/internal/handler/first_token_timeout_integration_test.go`

- [x] **Step 1: 写入口级失败测试，证明 metadata 不提交且 timeout 可换号**

为 Responses、Chat Completions、Messages 各建立两个 `httptest.Server` 账号：第一个立即发 headers/metadata/ping 后阻塞，第二个发送语义 delta。断言客户端只看到第二账号 header/body，且第一个 request context 被取消。

- [x] **Step 2: 运行三入口测试并确认旧实现失败**

Run: `cd backend && go test ./internal/handler -run 'TestFirstTokenTimeout_(Responses|ChatCompletions|Messages)_Failover' -count=1`

Expected: FAIL，旧实现会提前写 metadata/header 或不会按阈值取消。

- [x] **Step 3: 实现 attempt runner helper**

```go
type FirstTokenAttemptMetadata struct {
    Protocol service.FirstTokenProtocol
    AccountID int64
    Platform string
    Model string
    AttemptIndex int
    SwitchCount int
}

func runFirstTokenAttempt(
    c *gin.Context,
    policy *service.FirstTokenTimeoutPolicy,
    meta FirstTokenAttemptMetadata,
    forward func(context.Context) (*service.ForwardResult, error),
) (*service.ForwardResult, error)
```

helper 在 disabled 时直接调用 forward；enabled 时替换 `c.Writer`，defer 恢复原 writer，使用 attempt context 调用 forward，并把 timeout/overflow 转换为 typed failover error。不要把账号释放、选号或 billing 移入 helper。

- [x] **Step 4: 在两套 handler 的 failover 循环加最小 hook**

在 `GatewayHandler` 与 `OpenAIGatewayHandler` 的 Responses/Chat/Messages 流式分支中，在 account slot 后调用 helper；非流式、WebSocket 和 image generation intent 保持原调用。writer size 比较必须针对恢复后的底层 writer。

- [x] **Step 5: 在所有客户端协议写出点调用 detector**

在已经完成入站协议转换、准备写 SSE data 的位置执行：

```go
if service.IsFirstSemanticToken(protocol, eventName, payload) {
    if !service.MarkFirstTokenFromContext(ctx) {
        return context.Cause(ctx)
    }
}
```

调用必须先于该语义事件写出；保留现有 keepalive、silent refusal、usage 与 `first_token_ms` 代码原样。

- [x] **Step 6: 运行入口和现有流回归测试**

Run: `cd backend && go test ./internal/handler ./internal/service -run 'FirstTokenTimeout|Stream|Compact|SilentRefusal|ChatCompletions|Messages' -count=1`

Expected: PASS；关闭策略时现有快照/流测试无变化。

- [x] **Step 7: 提交**

```bash
git add backend/internal/handler/first_token_attempt_runner.go backend/internal/handler/first_token_attempt_runner_test.go backend/internal/handler/first_token_timeout_integration_test.go backend/internal/handler/gateway_handler_responses.go backend/internal/handler/gateway_handler_chat_completions.go backend/internal/handler/gateway_handler.go backend/internal/handler/openai_gateway_handler.go backend/internal/handler/openai_chat_completions.go backend/internal/service/gateway_forward_as_responses.go backend/internal/service/gateway_forward_as_chat_completions.go backend/internal/service/gateway_anthropic_passthrough.go backend/internal/service/gemini_chat_completions_compat_service.go backend/internal/service/gemini_messages_compat_service.go backend/internal/service/antigravity_gateway_streaming.go backend/internal/service/bedrock_stream.go backend/internal/service/openai_gateway_forward.go backend/internal/service/openai_gateway_passthrough.go backend/internal/service/openai_gateway_response_handling.go backend/internal/service/openai_gateway_cc_pipeline.go backend/internal/service/openai_gateway_chat_completions.go backend/internal/service/openai_gateway_chat_completions_raw.go backend/internal/service/openai_gateway_messages.go backend/internal/service/openai_gateway_messages_chat_fallback.go backend/internal/service/openai_gateway_responses_chat_fallback.go
git commit -m "feat: enforce first token timeout across http streams"
```

提交前用 `git diff --stat` 和 `git diff --check` 确认未格式化无关 service 文件。

### Task 5: Typed failover、客户端错误、调度与计费（OpenSpec 4.1、4.3-4.4）

**Files:**
- Modify: `backend/internal/service/gateway_service.go`
- Modify: `backend/internal/handler/failover_loop.go`
- Modify: `backend/internal/handler/gateway_handler_responses.go`
- Modify: `backend/internal/handler/gateway_handler_chat_completions.go`
- Modify: `backend/internal/handler/openai_gateway_handler.go`
- Modify: `backend/internal/handler/openai_chat_completions.go`
- Test: `backend/internal/handler/first_token_timeout_failover_test.go`
- Test: `backend/internal/service/first_token_timeout_billing_test.go`

- [x] **Step 1: 写耗尽、禁止同账号 retry 和零计费失败测试**

```go
func TestFirstTokenTimeoutFailoverError(t *testing.T) {
    err := NewFirstTokenTimeoutFailoverError()
    require.Equal(t, 504, err.StatusCode)
    require.Equal(t, "first_token_timeout", err.ErrorType)
    require.False(t, err.RetryableOnSameAccount)
}
```

分别断言三入口耗尽 envelope 的 HTTP status/type；pool mode retry count 非零时仍换号；timeout attempt 的 usage/balance 未变化，后续成功 attempt 只计费一次。

- [x] **Step 2: 运行并确认失败**

Run: `cd backend && go test ./internal/handler ./internal/service -run 'FirstTokenTimeout.*(Exhausted|Retry|Billing|FailoverError)' -count=1`

Expected: FAIL，缺少稳定 error type 或耗尽分支仍返回 `server_error`。

- [x] **Step 3: 最小扩展 `UpstreamFailoverError` 与 exhaustion mapping**

```go
type UpstreamFailoverError struct {
    StatusCode int
    ErrorType string
    // existing fields unchanged
}
```

空 `ErrorType` 保持原行为；`first_token_timeout` 专用 helper 返回 504。三种 handler exhaustion 只 special-case 此 type，其他 silent refusal/passthrough 映射不动。

- [x] **Step 4: 记录安全结构化事件与 scheduler failure**

日志只写 protocol/platform/account/model/threshold/attempt/switch/elapsed；不写 body、credential、URL。调用现有 scheduler runtime failure 上报，但跳过 temp unschedule 和持久账号状态修改。

- [x] **Step 5: 运行 failover、usage 与 billing 回归**

Run: `cd backend && go test ./internal/handler ./internal/service -run 'Failover|FirstTokenTimeout|RecordUsage|Billing' -count=1`

Expected: PASS；既有非 TTFT failover 行为不变。

- [x] **Step 6: 提交**

```bash
git add backend/internal/service/gateway_service.go backend/internal/handler/failover_loop.go backend/internal/handler/gateway_handler_responses.go backend/internal/handler/gateway_handler_chat_completions.go backend/internal/handler/openai_gateway_handler.go backend/internal/handler/openai_chat_completions.go backend/internal/handler/first_token_timeout_failover_test.go backend/internal/service/first_token_timeout_billing_test.go
git commit -m "feat: map first token timeout failover errors"
```

### Task 6: 小时聚合 migration 与 repository（OpenSpec 5.1-5.2）

**Files:**
- Create: `backend/migrations/175_first_token_timeout_stats_hourly.sql`
- Create: `backend/migrations/first_token_timeout_stats_hourly_test.go`
- Create: `backend/internal/service/first_token_timeout_stats.go`
- Create: `backend/internal/repository/first_token_timeout_stats_repo.go`
- Create: `backend/internal/repository/first_token_timeout_stats_repo_test.go`
- Modify: `backend/internal/repository/wire.go`

- [x] **Step 1: 写 migration 结构与 UPSERT 累加失败测试**

测试 migration 包含复合主键、scope/outcome/check、`ttft_affected_count`、两个查询索引；repository 测试把两个增量写入同 key，断言 count/sum 相加、max 取较大值。

- [x] **Step 2: 运行并确认失败**

Run: `cd backend && go test ./migrations ./internal/repository -run 'FirstTokenTimeoutStats' -count=1`

Expected: FAIL，migration/repository 不存在。

- [x] **Step 3: 创建 migration 与统计 port**

按 Design Doc 第 9 节创建表。request 统一 `account_id=0/platform=''`，无 account 外键。定义：

```go
type FirstTokenStatsDelta struct {
    BucketStart time.Time
    Scope, Protocol, Platform, Model, Outcome, FailureKind string
    AccountID int64
    TimeoutSeconds int
    SampleCount, TTFTSampleCount, TTFTSumMS, TTFTAffectedCount int64
    TTFTMaxMS int
}

type FirstTokenTimeoutStatsRepository interface {
    UpsertBatch(context.Context, []FirstTokenStatsDelta) error
    QueryOverview(context.Context, FirstTokenStatsFilter) (*FirstTokenStatsOverview, error)
    QueryAccounts(context.Context, FirstTokenAccountStatsFilter) (*FirstTokenAccountStatsPage, error)
    DeleteBefore(context.Context, time.Time) (int64, error)
}
```

- [x] **Step 4: 实现批量 UPSERT 与查询**

使用参数化 SQL；计数加法、max `GREATEST`。overview 一次返回 summary、hourly trend、other failure distribution；account query left join accounts，支持白名单排序和分页。rate 在 service/query mapper 计算，零分母返回 0。

- [x] **Step 5: 覆盖阈值快照、client canceled 排除、TTFT 后其他失败分母和 90 天清理**

构造固定 UTC 小时数据，断言 request recovery denominator 使用 `ttft_affected_count`，account failure denominator 不含 canceled，阈值不同形成两个 key，DeleteBefore 只删 cutoff 前桶。

- [x] **Step 6: 运行测试并提交**

Run: `cd backend && go test ./migrations ./internal/repository -run 'FirstTokenTimeoutStats' -count=1`

Expected: PASS。

```bash
git add backend/migrations/175_first_token_timeout_stats_hourly.sql backend/migrations/first_token_timeout_stats_hourly_test.go backend/internal/service/first_token_timeout_stats.go backend/internal/repository/first_token_timeout_stats_repo.go backend/internal/repository/first_token_timeout_stats_repo_test.go backend/internal/repository/wire.go
git commit -m "feat: store hourly first token timeout stats"
```

### Task 7: Tracker、recorder、失败分类与生命周期接入（OpenSpec 5.3-5.4）

**Files:**
- Create: `backend/internal/service/first_token_timeout_tracking.go`
- Create: `backend/internal/service/first_token_timeout_tracking_test.go`
- Create: `backend/internal/service/first_token_timeout_stats_recorder.go`
- Create: `backend/internal/service/first_token_timeout_stats_recorder_test.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `backend/internal/handler/first_token_attempt_runner.go`
- Modify: `backend/internal/handler/gateway_handler_responses.go`
- Modify: `backend/internal/handler/gateway_handler_chat_completions.go`
- Modify: `backend/internal/handler/gateway_handler.go`
- Modify: `backend/internal/handler/openai_gateway_handler.go`
- Modify: `backend/internal/handler/openai_chat_completions.go`
- Modify: `backend/cmd/server/wire_gen.go`

- [x] **Step 1: 写恰好一次 outcome 与分类失败测试**

断言 timeout+success => 2 attempt rows + 1 `recovered_after_ttft` request；timeout+other failure => request `other_failure` 且 affected=1；客户端取消 outcome 存在但不进入 rate；重复 Finish 只产生一次 event。

- [x] **Step 2: 写 recorder 非阻塞与故障健康失败测试**

用容量 1 queue 和阻塞 fake repo：第三次 Record 必须立即返回并增加 dropped；flush error 不返回到请求调用方；成功后 lastSuccessfulFlush 更新但 dropped 不清零；Stop 最多等待 2 秒。

- [x] **Step 3: 运行并确认失败**

Run: `cd backend && go test ./internal/service -run 'FirstToken(Tracking|StatsRecorder|FailureKind)' -count=1`

Expected: FAIL，tracker/recorder 未定义。

- [x] **Step 4: 实现 tracker 与固定优先级分类**

```go
type FirstTokenRequestTracker struct {
    once sync.Once
    hadTTFT atomic.Bool
    finalThreshold atomic.Int64
    recorder FirstTokenStatsRecorder
}

func (t *FirstTokenRequestTracker) BeginAttempt(meta FirstTokenAttemptStatsMeta) *FirstTokenAttemptTracker
func (t *FirstTokenRequestTracker) Finish(err error, clientCanceled bool)
```

按 design 的 rate_limit/auth/4xx/5xx/transport/stream_idle_timeout/protocol/other 顺序分类；TTFT 与 client cancel 先短路。

- [x] **Step 5: 实现 recorder**

4096 channel、1000 unique keys、5 秒 ticker、2 秒 flush context、map swap、失败批次按 sample count 增加 dropped、每日 DeleteBefore(now-90d)、健康快照。Record 只用 non-blocking select。

- [x] **Step 6: 接入 handler request/attempt 终结**

只在 enabled eligible request 创建 request tracker；attempt runner 每次实际发起上游时创建/Finish attempt；handler 所有最终 return 通过单一 defer Finish request。语义 commit 保存真实 TTFT；timeout 不把阈值写成 TTFT sample。

- [x] **Step 7: 运行 race 与生命周期测试**

Run: `cd backend && go test -race ./internal/service ./internal/handler -run 'FirstToken(Tracking|StatsRecorder|Timeout)' -count=1`

Expected: PASS，无重复 outcome、无 race。

- [x] **Step 8: 生成 Wire 并提交**

Run: `cd backend && go generate ./cmd/server`

```bash
git add backend/internal/service/first_token_timeout_tracking.go backend/internal/service/first_token_timeout_tracking_test.go backend/internal/service/first_token_timeout_stats_recorder.go backend/internal/service/first_token_timeout_stats_recorder_test.go backend/internal/service/wire.go backend/internal/handler/first_token_attempt_runner.go backend/internal/handler/gateway_handler_responses.go backend/internal/handler/gateway_handler_chat_completions.go backend/internal/handler/gateway_handler.go backend/internal/handler/openai_gateway_handler.go backend/internal/handler/openai_chat_completions.go backend/cmd/server/wire_gen.go
git commit -m "feat: record first token timeout outcomes"
```

### Task 8: 独立管理员 settings/stats API（OpenSpec 1.2、5.5）

**Files:**
- Create: `backend/internal/handler/admin/first_token_timeout_handler.go`
- Create: `backend/internal/handler/admin/first_token_timeout_handler_test.go`
- Modify: `backend/internal/handler/handler.go`
- Modify: `backend/internal/handler/wire.go`
- Modify: `backend/internal/server/routes/admin.go`
- Modify: `backend/cmd/server/wire_gen.go`
- Modify: `backend/internal/server/api_contract_test.go`

- [ ] **Step 1: 写 API 鉴权后 handler 失败测试**

覆盖 GET/PUT settings、非法 0/301、overview 默认 24h、非法 range/protocol、accounts search/sort/page、degraded completeness。响应 rate 必须包含 `numerator/denominator/rate`。

- [ ] **Step 2: 运行并确认失败**

Run: `cd backend && go test ./internal/handler/admin ./internal/server -run 'FirstTokenTimeout|TTFT' -count=1`

Expected: FAIL，路由或 handler 不存在。

- [ ] **Step 3: 实现独立 `FirstTokenTimeoutHandler`**

```go
type FirstTokenTimeoutHandler struct {
    policy *service.FirstTokenTimeoutPolicy
    repo service.FirstTokenTimeoutStatsRepository
    recorder *service.FirstTokenTimeoutStatsRecorder
}

func (h *FirstTokenTimeoutHandler) GetSettings(*gin.Context)
func (h *FirstTokenTimeoutHandler) UpdateSettings(*gin.Context)
func (h *FirstTokenTimeoutHandler) GetOverview(*gin.Context)
func (h *FirstTokenTimeoutHandler) GetAccounts(*gin.Context)
```

API DTO 不暴露内部错误或 SQL。Update 成功返回持久值、effective snapshot 与 loaded_at。

- [ ] **Step 4: 注册追加式路由与 Wire**

```go
settings.GET("/first-token-timeout", h.Admin.FirstTokenTimeout.GetSettings)
settings.PUT("/first-token-timeout", h.Admin.FirstTokenTimeout.UpdateSettings)
ttft := admin.Group("/ttft")
ttft.GET("/overview", h.Admin.FirstTokenTimeout.GetOverview)
ttft.GET("/accounts", h.Admin.FirstTokenTimeout.GetAccounts)
```

只给 `AdminHandlers`、`ProvideAdminHandlers`、ProviderSet 追加一个字段/provider。

- [ ] **Step 5: 更新 contract test、生成 Wire 并运行测试**

Run: `cd backend && go generate ./cmd/server && go test ./internal/handler/admin ./internal/server -run 'FirstTokenTimeout|TTFT|APIContract' -count=1`

Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add backend/internal/handler/admin/first_token_timeout_handler.go backend/internal/handler/admin/first_token_timeout_handler_test.go backend/internal/handler/handler.go backend/internal/handler/wire.go backend/internal/server/routes/admin.go backend/cmd/server/wire_gen.go backend/internal/server/api_contract_test.go
git commit -m "feat: expose first token timeout admin APIs"
```

### Task 9: `/admin/ttft` 页面（OpenSpec 6.1-6.5）

**Files:**
- Create: `frontend/src/api/admin/ttft.ts`
- Create: `frontend/src/views/admin/ttft/FirstTokenTimeoutView.vue`
- Create: `frontend/src/views/admin/ttft/components/TTFTSettingsBar.vue`
- Create: `frontend/src/views/admin/ttft/components/TTFTSummaryMetrics.vue`
- Create: `frontend/src/views/admin/ttft/components/TTFTFailureTrendChart.vue`
- Create: `frontend/src/views/admin/ttft/components/TTFTFailureDistributionChart.vue`
- Create: `frontend/src/views/admin/ttft/components/TTFTAccountStatsTable.vue`
- Create: `frontend/src/views/admin/ttft/__tests__/FirstTokenTimeoutView.spec.ts`
- Create: `frontend/src/i18n/locales/zh/admin/ttft.ts`
- Create: `frontend/src/i18n/locales/en/admin/ttft.ts`
- Modify: `frontend/src/api/admin/index.ts`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/components/layout/AppSidebar.vue`
- Modify: `frontend/src/i18n/locales/zh/admin/index.ts`
- Modify: `frontend/src/i18n/locales/en/admin/index.ts`
- Modify: `frontend/src/i18n/locales/zh/common.ts`
- Modify: `frontend/src/i18n/locales/en/common.ts`

- [ ] **Step 1: 写 API types 和页面行为失败测试**

mock `/settings/first-token-timeout`、`/ttft/overview`、`/ttft/accounts`；断言默认 query `range=24h`，设置保存保留统计，所有 rate 显示 `numerator / denominator`，账号筛选只重载 accounts，degraded 显示警告。

- [ ] **Step 2: 运行并确认失败**

Run: `cd frontend && pnpm test:run src/views/admin/ttft/__tests__/FirstTokenTimeoutView.spec.ts`

Expected: FAIL，页面/API 模块不存在。

- [ ] **Step 3: 实现 API 类型**

```ts
export interface RateMetric { numerator: number; denominator: number; rate: number }
export type TTFTRange = '24h' | '7d' | '30d' | '90d'
export interface TTFTCompleteness {
  status: 'complete' | 'degraded'
  dropped_samples: number
  last_successful_flush_at: string | null
  pending_samples: number
}
```

提供 `get/updateSettings`、`getOverview`、`getAccounts`，不把类型并入大型 `settings.ts` 或 `ops.ts`。

- [ ] **Step 4: 实现页面设置带、筛选和独立加载状态**

使用现有 AppLayout/Base 组件。URL query 同步 range/protocol/model；overview/accounts 分开 loading/error/retry。账号 search/platform/account/sort/page 只传 accounts API。

- [ ] **Step 5: 实现五指标、折线、横条和账号表**

图表使用现有 Chart.js/vue-chartjs；线型与颜色同时区分。表展示账号/平台、非取消 samples、success、TTFT count/rate、other count/rate、avg TTFT、`samples < 20` 低样本标记。

- [ ] **Step 6: 增加 route/sidebar/双语 locale 与完整状态测试**

路由 `/admin/ttft` 位于 Ops 后；sidebar label 为 `nav.ttftMonitoring`。覆盖 skeleton、empty、overview error、accounts error、degraded、dark class 与窄屏横向滚动容器。

- [ ] **Step 7: 运行前端测试、类型检查和生产构建**

Run: `cd frontend && pnpm test:run src/views/admin/ttft/__tests__/FirstTokenTimeoutView.spec.ts && pnpm typecheck && pnpm build`

Expected: 全部 PASS，Vite build 成功。

- [ ] **Step 8: 提交**

```bash
git add frontend/src/api/admin/ttft.ts frontend/src/api/admin/index.ts frontend/src/views/admin/ttft frontend/src/router/index.ts frontend/src/components/layout/AppSidebar.vue frontend/src/i18n/locales
git commit -m "feat: add first token monitoring admin page"
```

### Task 10: 端到端回归、文档同步与发布保护（OpenSpec 7.1-7.3）

**Files:**
- Modify: `backend/internal/handler/first_token_timeout_integration_test.go`
- Modify: `backend/internal/service/first_token_timeout_stats_recorder_test.go`
- Modify: `frontend/src/views/admin/ttft/__tests__/FirstTokenTimeoutView.spec.ts`
- Modify: `openspec/changes/configurable-first-token-timeout-failover/tasks.md`
- Modify: `docs/superpowers/plans/2026-07-14-first-token-timeout-failover.md`

- [ ] **Step 1: 补上候选耗尽、客户端取消和首 token 后停流失败测试**

断言耗尽为 504；客户端断开后 upstream context canceled 且 selector 不再调用；首 token commit 后 idle timeout 不换号；失败 attempt 的 header/body 字节数为零。

- [ ] **Step 2: 补上全部排除范围与零统计失败测试**

覆盖 disabled、non-stream、WebSocket、image generation intent、video、batch/background，断言原 forward path 被调用且 recorder event count 为零。

- [ ] **Step 3: 运行目标测试并修正仅由本 change 引入的问题**

Run: `cd backend && go test -race ./internal/service ./internal/handler ./internal/repository -run 'FirstToken|TTFT' -count=1`

Expected: PASS。若失败，先加载 systematic-debugging，定位根因后补最小失败测试再修改。

- [ ] **Step 4: 运行后端 migration 与全量测试**

Run: `cd backend && go test ./migrations -count=1 && go test ./... -count=1`

Expected: PASS，无 migration 重号/语法/全包回归。

- [ ] **Step 5: 运行前端全量验证**

Run: `cd frontend && pnpm lint:check && pnpm typecheck && pnpm test:run && pnpm build`

Expected: 全部 PASS。

- [ ] **Step 6: 检查低冲突边界和敏感数据**

Run: `git diff --check && git diff --stat e15265205a50addfeba66f935b7e256ea2a51f20...HEAD && rg -n 'T[B]D|T[O]DO|first_token_timeout.*(body|credential|token=)' backend/internal frontend/src`

Expected: 无 whitespace error、无占位符、日志/统计无正文凭据；大型文件只有必要 hook。

- [ ] **Step 7: 同步 OpenSpec 与 plan 勾选并提交验证补充**

确认本计划所有任务和 `openspec/.../tasks.md` 1.1-7.3 均为 `[x]`，且未把用户的 `api_key` 加入 Git。

```bash
git add backend frontend openspec/changes/configurable-first-token-timeout-failover/tasks.md docs/superpowers/plans/2026-07-14-first-token-timeout-failover.md
git commit -m "test: verify first token timeout failover"
```

## Spec Coverage 自审

- 设置/热更新：Task 1、Task 8、Task 9。
- 状态机/response gate：Task 2。
- 三协议 detector 与所有 HTTP pipeline：Task 3、Task 4。
- failover/504/调度/计费：Task 4、Task 5、Task 10。
- migration/repository/90 天保留：Task 6、Task 7。
- attempt/request outcome、分母、failure kind、completeness：Task 6-8。
- 独立管理员页面、图表、账号率、筛选隔离和完整状态：Task 9。
- 排除范围、全量验证和默认关闭回滚：Task 10。

计划没有新增第三方依赖，没有修改现有 `first_token_ms` 口径，没有把账号筛选应用到 request 指标，也没有要求重构现有大型流 pipeline。
