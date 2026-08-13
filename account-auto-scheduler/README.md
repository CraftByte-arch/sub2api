# Sub2API 账号自动调度

这是一个独立旁路服务。它不读取 Sub2API 数据库，也不导入 Sub2API 后端代码。自动检测和调度只作用于 `type=apikey` 的账号；管理页面通过现有 HTTP 接口汇总所有分组和账号：

- `GET /api/v1/admin/accounts?type=apikey`：同步可配置账号和当前调度状态
- `GET /api/v1/admin/accounts`、`GET /api/v1/admin/groups/all`：生成分组账号总览
- `POST /api/v1/admin/accounts/today-stats/batch`：读取账号今日请求、Token 和成本
- `GET /api/v1/admin/accounts/:id/usage?source=passive`：按需读取 OAuth 账号额度快照
- `PUT /api/v1/admin/accounts/:id`：保留其他分组关系，只增删当前分组绑定
- `POST /api/v1/admin/accounts/:id/test`：未授权直连探测的旧配置继续使用该接口检测
- `GET /api/v1/admin/accounts/data?ids=:id&include_proxies=true`：仅在管理员显式授权直连探测时，使用当前浏览器 JWT 通过原有 step-up 校验导入单个账号
- `POST /api/v1/admin/accounts/:id/schedulable`：关闭或恢复账号调度
- `GET /api/v1/auth/me`：验证嵌入页访问者仍是管理员
- `GET/PUT /api/v1/admin/settings`：注册管理员专属自定义菜单 Tab
- `PUT/DELETE /api/upstreams/:id/recharge-rate`：设置或清除旁路状态中的上游充值倍率，不修改 Sub2API 数据

## 管理页面

- 首次默认展开全部分组，并直接列出每组的 API Key 账号、实时可用数、调度状态及原因；管理员可以展开或收起分组，浏览器会保留折叠偏好。
- OAuth 和 Setup Token 账号按组折叠为数量入口，弹窗每页显示 10 个账号及今日用量、被动额度快照。
- 每组内当前可用账号优先排列，其余账号再按名称和 ID 稳定排序。
- API Key 账号显示今日用量、管理员配置的日/周/总额度、上游列表中已显式绑定 Key 的最终倍率、检测规则和最近 50 次结果。最终倍率只在充值倍率和上游分组倍率均已同步时显示；未绑定、绑定失效、存在歧义或缺少配置时会显示原因，不会回退显示 Sub2API 探测倍率。
- 每个 API Key 账号都有自动调度开关；无配置账号开启时创建默认检测规则，已有规则则保留原配置。
- 检测色块沿用渠道监控口径：成功且低于 `6000ms` 为绿色，成功且达到 `6000ms` 或耗时超限为黄色，失败为红色，无历史为灰色。
- 每个真实分组都有“管理账号”入口：弹窗只列出与分组平台兼容的 API Key 账号，Composite 分组可接收所有具体平台账号，Anthropic/Gemini 分组按现有规则接收已开启混合调度的 Antigravity 账号；已存在的跨平台旧绑定仍会显示并可解绑。
- 分组账号以复选框批量编辑，浏览器一次提交最终选择；旁路服务仍逐账号调用 Sub2API 原有更新接口并保留其他分组关系，不会自动确认或绕过混合渠道风险。部分失败会保留待重试项和失败原因。

## 直连上游状态检测

直连探测是每个 API Key 检测配置上的显式授权，不会在升级后自动开启。旧配置仍通过 Sub2API 的账号检测接口；管理员在“编辑检测规则”中授权后，该账号才改为由旁路服务直接流式调用其上游。撤销授权会删除本地加密快照，并让该配置恢复使用 Sub2API 检测。

- 仅支持 API Key 类型的 OpenAI、Anthropic、Gemini、Grok 和 Antigravity 账号。OAuth 与 Setup Token 账号不能授权直连探测。
- OpenAI 与 Grok 使用 OpenAI-compatible 流式接口；Anthropic 使用 Messages SSE；Gemini 使用 `streamGenerateContent` SSE；Antigravity 的 `gemini-*` 模型走 Gemini，其余支持模型走 Anthropic。
- 探测使用检测规则中配置的模型和提示词。自定义提示词会原样发送；默认提示词会生成并校验一道算术题。完整流式响应达到或超过耗时上限时按现有降级规则计数。
- 直连请求保留账号的模型映射、允许的请求头覆盖和受支持代理。账号路由或代理配置变化、快照缺失、代理不可用或快照无法解密时，页面会要求重新授权；这类跳过不会累计失败/恢复次数，也不会改变调度状态。
- 已选择直连的账号发生上游、传输、超时或流协议错误时不会回退调用 Sub2API 账号检测接口，避免一次检测走两条不同路径。
- 非成功 HTTP 响应和 SSE 错误会在检测历史中显示状态码及安全的上游 `message/code/type` 详情；API Key、Bearer/JWT、Cookie、代理凭证、上游地址和自定义请求头值会在保存前脱敏。

授权接口先验证当前浏览器 JWT 仍属于管理员，再把该 JWT、客户端 IP 和 User-Agent 临时转发给 Sub2API 的单账号导出接口。是否需要二次验证完全由 Sub2API 的现有 step-up 策略决定；旁路服务不会使用环境中的 Admin API Key 绕过导出校验，也不会持久化浏览器 JWT。

导入内容只保留直连所需的上游地址、API Key、平台、模型映射、过滤后的请求头和受支持代理，并使用 `AUTO_SCHEDULER_CREDENTIAL_KEY` 以 AES-256-GCM 加密。直连快照使用绑定账号 ID 的独立 `direct-probe-credential/v1` 关联数据，与上游登录会话密文不能互相解密。浏览器响应、DOM、日志和公开状态不会包含 API Key、原始请求头、代理密码、JWT 或密文。执行探测时，API Key 只从服务器发送到该账号配置的上游或指定代理；若上游在公网，请使用可信 HTTPS 端点。

重新授权会通过 step-up 导出当前账号并原子替换旧快照。轮换 `AUTO_SCHEDULER_CREDENTIAL_KEY` 后，旧直连快照和上游登录会话都无法用新密钥解密；服务会保留检测配置并标记需要重新授权，不会把解密失败计作账号故障。

## 上游列表

管理员页面内的二级 Tab 包含“分组与账号”和“上游列表”。只有第一次打开“上游列表”时才会读取上游数据；原分组筛选、展开状态和账号配置不会被重置。

- 本地候选只来自 `type=apikey` 账号的脱敏 `base_url`。相同部署根地址会合并为一个上游，OAuth 和 Setup Token 凭证不参与关联。
- 上游可以匿名自动识别为 Sub2API 或 NewAPI，也可以由管理员手动指定类型。识别请求不会携带登录凭证。
- `API 地址`始终用于实际模型调用和本地 API Key 账号关联。连接或重新连接身份时可填写可选的`管理站点地址`；登录、身份校验、会话刷新、余额、Key 和倍率同步都通过它完成，留空时才回退到 API 地址。成功验证后才会保存该覆盖地址，因此错误地址不会替换已有可用设置。
- 同一个上游可以保存多个登录身份，支持账号密码、访问 Token、Cookie/session 三种方式。密码只用于本次登录，验证结束后不会保存。
- 账号密码模式要求手动填写目标上游自己的凭据，不复用当前 Sub2API 后台的自动填充密码；旧版 NewAPI 优先使用站内用户名。上游明确拒绝账号或密码时，页面会显示专用提示，不再归为模糊的协议错误。
- 展开上游后可查看每个登录身份的实际站点余额、最近获取时间、本地账号、登录状态、远端 Key 状态、分组、用量、额度、固定倍率以及 NewAPI `auto` 分组的最近动态倍率观测。余额属于登录身份；同一站点存在多个身份时分别展示，上游摘要只显示有余额的账号数量，绝不合计。
- 每个上游可以手动设置充值倍率，支持 `1 CNY = N USD` 和 `N CNY = 1 USD` 两种输入。服务统一保存为 `CNY / USD`，每个 Key 的最终倍率为 `充值倍率 × 分组倍率`；例如 `1 CNY = 5 USD` 与分组倍率 `0.8` 得到 `0.2 × 0.8 = 0.16`。
- 旧版 NewAPI 的账号密码登录会自动从登录响应取得用户 ID 并发送 `New-Api-User`；手动 Token 或 Cookie/session 模式可填写个人中心显示的数字用户 ID。用户 ID 与会话材料一起加密，不会出现在上游列表响应中。
- 远端 Key 可以手动批量绑定到同一上游的本地 API Key 账号。自动匹配是单独的敏感操作，只接受完整 Key 的唯一精确匹配；掩码 Key、重复 Key 或不确定结果不会猜测绑定。
- CAPTCHA、Cloudflare、人机验证和二次验证不会被绕过。请先在上游网站完成交互式登录，再粘贴可复用的 Token 或 Cookie/session。

### 支持的上游接口

当前适配常见的 Sub2API 与 NewAPI HTTP 合约；派生版本若更改路径、鉴权格式或响应结构，页面会保留最近一次成功快照并显示同步失败。

| 网站类型 | 匿名识别 | 登录、验证与站点余额 | Key、额度、用量与倍率 |
| --- | --- | --- | --- |
| Sub2API | `GET /api/v1/settings/public` | `POST /api/v1/auth/login`、`GET /api/v1/auth/me` 的 `balance`、`POST /api/v1/auth/refresh` | `GET /api/v1/keys`、`GET /api/v1/groups/available`、`GET /api/v1/groups/rates` |
| NewAPI | `GET /api/status` 的 `quota_per_unit`，必要时 `GET /api/user/groups` | `POST /api/user/login`、`GET /api/user/self` 的 `quota`、`POST /api/user/auth/refresh` | `GET /api/token/`、`GET /api/user/self/groups`、`GET /api/log/self` |

同步只读取远端信息，不创建、修改、删除或显示完整上游 Key，也不会把探测到的倍率写回 Sub2API 账号。NewAPI `auto` 分组没有明确的近期消费日志时只显示“动态”，不会用 `0` 代替未知倍率。

充值倍率属于本旁路服务自己的管理员配置，不从上游猜测，也不写回 Sub2API 或上游站点。分组倍率或充值倍率任一未知时，页面明确显示“未计算”，不会用 `0` 或默认值代替。

Sub2API 用户资料返回的有限 `balance` 直接按 USD 展示，包括真实的 `0 USD`。NewAPI 用户资料返回内部 `quota`；旁路服务仅在同一上游的匿名 `/api/status` 发布正数且有限的 `quota_per_unit` 时按 `quota / quota_per_unit` 换算为 USD，并在快照中保留换算元数据。换算参数缺失或无效时只显示原始 `quota`，不会猜测货币。手动配置的充值倍率表达管理员的采购成本，不参与站点余额换算。

### 同步时序

- 打开“上游列表”只加载已有候选和快照，不会自动登录上游。
- 登录成功后可单独同步身份、同步一个上游或手动同步全部上游；这些现有动作会同时刷新 Key 与该身份的站点余额，不新增余额专用请求。
- `AUTO_SCHEDULER_UPSTREAM_SYNC_SECONDS` 默认是 `600` 秒；设置为 `0` 关闭周期同步，非零值必须在 `60` 到 `86400` 秒之间。
- 周期同步与账号健康检测、页面 10 秒概览刷新完全独立。一个身份失败只更新该身份的错误和尝试时间，其他身份及最近成功快照继续保留；失败身份的最后余额保留原观测时间并明确标记为旧数据，下一次成功同步后才替换并恢复为当前数据。

### 凭证安全与密钥轮换

`AUTO_SCHEDULER_CREDENTIAL_KEY` 是旁路敏感凭证专用的 32 字节密钥，同时保护上游登录会话和直连探测快照；接受 base64 或 64 位 hex 编码。不要复用 Sub2API Admin API Key。生成 base64 密钥：

```bash
openssl rand -base64 32
```

可复用的访问 Token、刷新 Token 和 Cookie 使用 AES-256-GCM 加密后写入状态文件，关联数据绑定上游 ID 与身份 ID。浏览器响应和日志不返回密文、完整 Key、Authorization、Cookie 或密码。认证请求有大小、超时和响应上限；带凭证的跨来源重定向会被拒绝，远端错误会被截断和脱敏。

不配置密钥时服务仍会启动，现有账号检测和自动调度保持工作，只有上游登录、凭证同步和自动匹配返回可操作的禁用提示。所有上游 API 仍要求当前浏览器的有效管理员 JWT；自动精确匹配还会把该 JWT 转发给 Sub2API 现有账号导出接口，由其二次验证策略决定是否放行，绝不会回退到服务端 Admin API Key。

当前版本不支持双密钥在线轮换。轮换前先备份状态文件并安全保留旧密钥，只修改旁路服务的 `.env`，然后仅重建 `account-auto-scheduler`。新密钥无法解密旧身份时，身份会标记无效但最近快照仍保留；管理员需逐个重新连接。若需要撤销轮换，恢复旧密钥并仅重启旁路服务。

## 行为边界

- 连续调用失败或耗时超限达到配置阈值后，设置 `schedulable=false`。
- 暂停调度后仍持续检测；连续正常达到恢复阈值后，设置 `schedulable=true`。
- 后台检测只恢复由本服务亲自暂停的账号，不会自行打开管理员手动停调的账号。管理员在本页面明确开启该账号的自动调度时，会先恢复调度，再交给检测规则自动管理。
- 停用或删除一条由本服务暂停的规则前，会先恢复该账号调度；恢复失败时拒绝停用或删除。
- 每个账号只保留最近 50 次检测结果，状态文件使用 `0600` 权限原子写入。

## 部署

1. 在现有 Sub2API 管理后台的系统设置中生成 Admin API Key。
2. 将 `.env.example` 复制为 `.env`，填写 Admin API Key、主服务地址和外部访问 URL。若启用上游连接或直连探测，再生成并填写独立的 `AUTO_SCHEDULER_CREDENTIAL_KEY`。
3. 启动独立服务：

```bash
docker compose -f compose.example.yml up -d --build
```

4. 将 `AUTO_SCHEDULER_PUBLIC_URL` 对应路径反向代理到 `127.0.0.1:8091`。路径代理必须保留末尾 `/` 并去掉前缀：

```nginx
location /account-auto-scheduler/ {
    proxy_pass http://127.0.0.1:8091/;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $remote_addr;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

启动时服务会通过现有设置接口添加一个 `visibility=admin` 的“账号自动调度”菜单项。注册是幂等的，不会删除或覆盖其他自定义菜单。若关闭 `AUTO_SCHEDULER_AUTO_REGISTER_TAB`，可以在页面右上角手动注册，或在系统设置的自定义菜单中添加 URL。

已部署环境更新旁路服务时，只重建它自己的容器：

```bash
docker compose -f compose.hc2.yml up -d --build --no-deps account-auto-scheduler
```

该命令不会重建或重启 Sub2API、Sub2API canary、Nginx UI 等现有容器。发布前后应记录这些容器的重启次数并确认未发生变化。

`hc2` 使用蓝绿槽位时，不要把 `SUB2API_BASE_URL` 固定为 `sub2api` 或 `sub2api-canary`。`compose.hc2.yml` 将 `aixw.org` 解析到 Docker host-gateway，远端 `.env` 应配置 `SUB2API_BASE_URL=https://aixw.org`，由本机 Nginx 始终转发到当前活动槽位。

首次部署直连探测前先备份状态文件，例如：

```bash
docker cp account-auto-scheduler:/data/state.json ./state.pre-direct-probe.json
```

状态文件会在下一次写入时从 v1 或 v2 原子迁移为 v3，并保留全部检测配置、最近 50 次历史、上游、登录身份和 Key 绑定。余额是 v3 身份记录上的可选字段，已有 v3 状态无余额字段时直接显示待登录/同步，无需迁移版本。迁移后的旧检测配置默认保持 `legacy`，不会自动导入账号或改成直连。旧版旁路服务不能读取 v3；回滚旧镜像时必须同时恢复部署前的状态备份。余额版本回滚只需替换此前的旁路镜像，旧版 v3 二进制会忽略新增余额字段；发布和回滚都只操作 `account-auto-scheduler`，不要重建 Sub2API、数据库、Redis、Nginx 或 Nginx UI。

hc2 发布前还应确认 `.env` 中已有非空且有效的 `AUTO_SCHEDULER_CREDENTIAL_KEY`，但检查过程不要输出密钥值。发布只允许执行：

```bash
docker compose -f compose.hc2.yml up -d --build --no-deps account-auto-scheduler
```

发布前后分别记录全部容器的名称和 restart count，并确认除 `account-auto-scheduler` 外没有容器被重建或重启。若需要回滚，只替换该旁路容器并恢复 `state.pre-direct-probe.json`；不能让 v2 二进制直接读取已迁移的 v3 状态。

## 管理员鉴权

后台任务使用只保存在服务端环境变量中的 Admin API Key。浏览器页面不会收到这把 Key；页面每次调用自身 API 时都会提交当前 Sub2API JWT，旁路服务再调用 `/auth/me` 验证 `role=admin`。

如果 Sub2API 开启了会话 IP/UA 绑定，并且本服务位于可信反向代理后，将 `AUTO_SCHEDULER_TRUST_PROXY_HEADERS=true`。只有在代理会覆盖客户端传入的 `X-Forwarded-For` 和 `X-Real-IP` 时才应开启该选项。

## 本地验证

```bash
node --check internal/web/static/app.js
node --check internal/web/static/upstreams.js
go fmt ./...
go test ./...
go test -race ./...
go run ./cmd/server
```

运行服务至少需要 `SUB2API_ADMIN_API_KEY`。默认监听 `:8091`，状态保存在 `./data/state.json`。
