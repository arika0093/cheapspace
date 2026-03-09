# Cheapspace 実装・アーキテクチャ計画

## 1. 現状整理
現時点のリポジトリには `README.md`、`SPECS.md`、`docs/plan` 配下の計画ファイルしか存在せず、アプリケーションコード、CI、Docker 定義、DB スキーマはまだ存在しない。したがって今回は既存実装の延長ではなく、SPECS.md と追加要件を満たすための初期アーキテクチャをゼロから定める必要がある。

今回の更新で反映すべき追加条件は次の通り。

- 実装言語は Go 方針を維持する。
- DB は raw SQL 寄りでもよいが、migration utility を導入する。
- workspace 内では Docker-in-Docker を提供しつつ、`--privileged` は避ける。
- Playwright による smoke E2E を追加する。
- 可能なら Traefik 等のラベル自動付与を行い、`*.sslip.io` 的な名前での到達性も考慮する。
- `docker compose up` はできるだけ Cheapspace 単体サービスで成立させる。
- Web UI は Tailwind CSS v4 を用い、HTMX ではない軽量な仕組みを採用する。
- SSH 公開鍵は複数指定可能にし、公開鍵未指定時には password login も提供する。
- `/var/run/docker.sock` マウント前提でよいが、Podman 等の Docker 互換 runtime に将来対応できる必要がある。
- workspace image と Cheapspace app image の両方が必要。
- job 一覧 / job 詳細 / log 表示 / 停止 / 再開 / 削除の UI が必要。
- environment setup は built-in image、manual Dockerfile、Nixpacks を扱える必要がある。

## 2. アーキテクチャ選定

### 2.1 採用スタック
**Go を主実装言語にした単一バイナリのモノリス構成**を維持しつつ、UI と DB 周りの採用技術を以下に更新する。

- 言語: Go
- Web: `net/http` + `chi`
- UI コンポーネント: `templ`
- Styling: `tailwindcss v4`
- 軽量インタラクション: `Stimulus` + `Server-Sent Events` / `fetch`
- DB: SQLite + `sqlc` + `goose`
- Container runtime 制御: Docker 互換 API abstraction（初期実装は Docker Engine、将来的に Podman compat socket 対応）
- 非同期処理: Go worker + 永続化 job テーブル + job log テーブル
- E2E: Playwright smoke test

### 2.2 Go + templ を選ぶ理由
1. **単一言語要件に最も素直に合う**  
   `templ` を使うと UI コンポーネントも Go コード生成ベースで管理でき、React/Next.js のような別アプリを持たずに済む。
2. **SSR と相性が良い**  
   workspace 一覧、詳細、job ログ、フォーム主体の UI は SSR で十分表現できる。初期表示が速く、compose 起動後すぐに使いやすい。
3. **動的部分だけを小さく閉じ込められる**  
   HTMX の代わりに Stimulus を使うことで、SSE 接続、フォーム補助、log tail 表示のような最小限の動作だけを薄い JavaScript controller に寄せられる。
4. **self-hostable な配布と運用に向く**  
   Go 単一バイナリに `serve` / `migrate` サブコマンドを入れれば、運用と復旧の手順が単純になる。

### 2.3 DB アクセス戦略
DB は ORM 中心ではなく、**SQL を明示的に書く方針**を採る。

- migration: `goose`
- query code generation: `sqlc`
- DB ドライバ: `modernc.org/sqlite` を優先候補

この構成にする理由は以下の通り。

- SQLite を使う都合上、実際のクエリや index 設計を明示的に管理したい。
- `sqlc` により raw SQL の見通しを保ちながら、Go 側は型安全に扱える。
- `goose` により migration 履歴、CLI、埋め込み実行を統一できる。
- Cheapspace バイナリに `migrate up`, `migrate status`, `migrate create` 相当の運用導線を持たせやすい。

### 2.4 単一サービス compose 方針
最小構成の `docker compose up` は以下だけで成立させる。

- `cheapspace` アプリコンテナ 1 つ
- named volume（SQLite DB, generated assets, temporary build metadata）
- `/var/run/docker.sock` の bind mount

つまり **必須 sidecar を持たない** 構成を基本とする。Traefik、Authelia、socket proxy 等はすべて optional integration とし、外部に既に存在する環境へ接続できるようにする。

## 3. システム構成

### 3.1 基本トポロジ
1. **cheapspace-app**  
   Go 製単一アプリ。HTTP UI、API、job 実行、runtime 制御、migration 実行を担当する。
2. **SQLite volume**  
   workspace メタデータ、鍵情報、job 状態、job log、temporary secrets を保持する。
3. **workspace containers**  
   実際の開発環境。cheapspace-app が Docker 互換 API を使って生成・削除・監視する。
4. **外部 reverse proxy / auth proxy（任意）**  
   Authelia や Traefik は Cheapspace の外側に置く。Cheapspace は必要に応じて label や ingress metadata を生成する。

### 3.2 コンテナ runtime 抽象化
Cheapspace 本体は `Docker Engine 固定実装` にせず、**Docker-compatible HTTP API を前提にした runtime interface** を持つ。

想定 interface 例:
- `CreateContainer`
- `StartContainer`
- `InspectContainer`
- `RemoveContainer`
- `BuildImage`
- `StreamLogs`
- `EnsureNetwork`
- `AllocatePort`
- `ApplyLabels`

初期 adapter:
- Docker Engine via `/var/run/docker.sock`

将来 adapter / compatibility target:
- Podman compat socket (`podman system service`)

この abstraction によって、アプリ内部では `internal/runtime` に閉じ込め、Docker 固有仕様が全体に漏れないようにする。

### 3.3 publish するイメージ
最低限、以下の 2 つを用意する。

1. **Cheapspace app image**  
   Go バイナリ、migration、static assets、必要な helper binary（例: `nixpacks`）を含む。
2. **Cheapspace workspace built-in image**  
   `devcontainers/universal` ベースで、Go / Docker CLI / rootless dockerd / sshd / tmux / Playwright 等を同梱する。

## 4. アプリ内部構造

### 4.1 推奨ディレクトリ構成
```text
cmd/cheapspace/
internal/config/
internal/db/
internal/db/sqlc/
internal/runtime/
internal/runtime/dockerapi/
internal/workspace/
internal/access/
internal/build/
internal/jobs/
internal/http/
internal/http/handlers/
internal/http/sse/
internal/ui/
web/assets/
web/templates/
migrations/
docker/app/
docker/workspace/
e2e/playwright/
.github/workflows/
```

### 4.2 モジュール責務
- `cmd/cheapspace`: `serve`, `migrate up`, `migrate status` などの CLI entrypoint。
- `internal/config`: 環境変数、runtime socket、resource 上限、ingress 設定を読む。
- `internal/db`: 接続初期化、migration 実行、repository wiring。
- `internal/db/sqlc`: hand-written SQL から生成された typed query 群。
- `internal/runtime`: Docker/Podman compat を吸収する interface と adapter。
- `internal/build`: built-in image / Dockerfile / Nixpacks の build strategy。
- `internal/workspace`: workspace state machine、入力検証、domain service。
- `internal/access`: SSH 鍵、password secret、ingress endpoint 組み立て。
- `internal/jobs`: job enqueue / lease / pause / cancel / retry / log append。
- `internal/http`: SSR 画面、form handling、JSON API、SSE endpoint。
- `internal/ui`: `templ` component と view model。
- `web/assets`: Tailwind v4 の entry CSS と軽量 JS controller。

## 5. データモデル

### 5.1 workspaces
workspace の中心テーブル。主なカラム案:

- `id`
- `name`
- `state` (`pending`, `building`, `provisioning`, `running`, `deleting`, `deleted`, `failed`, `expired`)
- `repo_url`
- `dotfiles_url`
- `environment_source_type` (`builtin_image`, `image_ref`, `dockerfile`, `nixpacks`)
- `environment_source_ref`
- `resolved_image_ref`
- `nixpacks_plan_json`
- `http_proxy`, `https_proxy`, `no_proxy`
- `cpu_limit_millis`
- `memory_limit_mb`
- `ttl_minutes`
- `runtime_kind` (`docker`, `podman-compat`)
- `ssh_endpoint_mode` (`host_port`, `traefik_tcp`, `ssh_tunnel_443`)
- `ssh_host`, `ssh_port`
- `public_hostname`
- `password_auth_enabled`
- `password_hash`
- `container_id`, `container_name`, `volume_name`, `network_name`
- `last_error`
- `created_at`, `started_at`, `expires_at`, `deleted_at`

### 5.2 workspace_ssh_keys
複数 SSH 公開鍵を扱うための正規化テーブル。

- `id`
- `workspace_id`
- `key_type`
- `public_key`
- `comment`
- `created_at`

### 5.3 workspace_events
状態遷移、ingress 付与、password 生成、失敗理由などの履歴を残す。

- `id`
- `workspace_id`
- `event_type`
- `message`
- `detail_json`
- `created_at`

### 5.4 jobs
非同期処理の本体。

- `id`
- `job_type` (`build_image`, `provision_workspace`, `delete_workspace`, `reconcile_workspace`, `expire_workspace`)
- `workspace_id`
- `status` (`queued`, `leased`, `running`, `pause_requested`, `paused`, `cancel_requested`, `cancelled`, `done`, `failed`)
- `payload_json`
- `attempt`
- `run_after`
- `lease_owner`
- `lease_expires_at`
- `stopped_by_user`
- `last_error`
- `created_at`, `updated_at`

### 5.5 job_logs
job 詳細画面で tail 表示するための append-only log テーブル。

- `id`
- `job_id`
- `sequence_no`
- `stream` (`stdout`, `stderr`, `system`)
- `message`
- `created_at`

### 5.6 port_leases
SSH host port 衝突回避用。

- `port`
- `workspace_id`
- `leased_at`
- `released_at`

### 5.7 ephemeral_secrets
one-time reveal 用の短期 secret を暗号化して保持する補助テーブル。

- `id`
- `workspace_id`
- `secret_kind` (`ssh_password`)
- `ciphertext`
- `expires_at`
- `consumed_at`
- `created_at`

## 6. Build source と environment setup

### 6.1 対応する build source
workspace 作成時の environment source は次の 4 種類を扱う。

1. **built-in image**  
   Cheapspace が提供する標準イメージを使う。
2. **image ref**  
   ユーザーが既存の OCI イメージ参照を直接指定する。
3. **manual Dockerfile**  
   フォームまたは参照先から Dockerfile を受け取り、runtime build API で image を build する。
4. **Nixpacks**  
   clone した repository に対して `nixpacks` を適用し、自動生成された build plan から image を作る。

### 6.2 Nixpacks 方針
- Cheapspace app image に `nixpacks` binary を同梱する。
- workspace create 時に repo clone 後、Nixpacks build job を実行する。
- build log は `job_logs` に逐次保存する。
- 生成された image ref は `workspaces.resolved_image_ref` に反映する。
- `nixpacks.toml` や detected plan の情報は `nixpacks_plan_json` と event log に記録する。

### 6.3 migration / build CLI 方針
単一バイナリ方針を保つため、運用用コマンドは Cheapspace 自身に持たせる。

想定 subcommand:
- `cheapspace serve`
- `cheapspace migrate up`
- `cheapspace migrate status`
- `cheapspace jobs run-once`（将来的デバッグ用）

## 7. workspace lifecycle と job モデル

### 7.1 作成フロー
1. ユーザーが新規作成フォームで source 種別、repo URL、dotfiles、SSH 鍵群、password login、proxy、resource 制限、TTL を送信する。
2. アプリが domain 層で入力値を検証する。
   - CPU / Memory / TTL 上限
   - repo URL / dotfiles URL
   - 複数 SSH 公開鍵の形式
   - password login の有効可否
3. `workspaces` を `pending` で保存し、`workspace_ssh_keys` を保存する。
4. 必要に応じて `build_image` job を enqueue する。
5. build 完了後に `provision_workspace` job が network / volume / container / ingress metadata を作る。
6. 準備完了後に `running` へ遷移し、SSH 接続情報、password one-time reveal、job ログを UI で見られるようにする。
7. 失敗時は `failed` に遷移し、job 詳細と event log にエラーを残す。

### 7.2 削除フロー
1. ユーザーが削除を要求する。
2. `delete_workspace` job を enqueue し、workspace を `deleting` にする。
3. worker が container / volume / port / temporary secret を破棄する。
4. 成功したら `deleted` に遷移する。

### 7.3 TTL / 再起動復旧
アプリ起動時に以下を実施する。

- `running`, `building`, `provisioning`, `deleting` workspace を走査
- runtime 実体と DB を照合
- 期限切れ workspace を `expire_workspace` job に送る
- lease 切れ job を recover する
- unconsumed one-time secret の期限を掃除する

### 7.4 停止 / 再開 / 削除の意味
job 制御は cooperative に行う。

- **停止**: running job に `cancel_requested` を立て、各ステップ境界で中断する。
- **再開**: `paused` または `failed` job を新規 attempt として再 enqueue する。
- **削除**: 完了済み・失敗済み job とその log を UI 上から掃除できるようにする。
- **一時停止**: queue 中または cooperative checkpoint を持つ job のみ対応する。

## 8. workspace image 設計

### 8.1 built-in image
`devcontainers/universal` をベースにした独自 image を用意し、最低限以下を含める。

- `openssh-server`
- `tmux`
- Git / common dev tools
- Playwright 実行に必要な browser / system dependency
- `@github/copilot`, `codex`, `claudecode` 等の CLI
- Docker CLI
- rootless Docker 実行に必要な `uidmap`, `slirp4netns`, `fuse-overlayfs`
- 必要に応じて rootless Podman / `podman-docker` 互換
- workspace 初期化 entrypoint

### 8.2 DinD 方針
workspace では **rootless Docker を既定の nested runtime** とする。

- `dockerd-rootless.sh` を `codespace` ユーザーで起動する
- storage driver は `fuse-overlayfs` を優先候補とする
- networking は `slirp4netns` ベースを前提にする
- `--privileged` は使わない
- host 側では必要最小限の追加設定のみ許可する（例: `/dev/fuse`、必要な seccomp/apparmor 緩和の検証）
- rootless Docker が成立しない環境では rootless Podman + `podman-docker` alias を fallback 候補にする

この方針により、「ユーザーに Docker を提供する」要件を満たしつつ、host docker.sock を workspace に直接見せない。

### 8.3 entrypoint の責務
1. `codespace` ユーザー作成
2. SSH 公開鍵群を `authorized_keys` に反映
3. password login が有効なら一時パスワードを設定
4. proxy を `/etc/profile.d` と shell rc に反映
5. repo clone
6. dotfiles clone + install
7. rootless dockerd / 代替 runtime を起動
8. `tmux` / `sshd` を起動
9. readiness シグナルを出す

### 8.4 SSH アクセス設計
- 公開鍵は複数指定可能
- password login は opt-in か、鍵未指定時に自動有効
- password は強ランダム生成し、UI では one-time reveal にする
- DB 永続化は `password_hash` と短期暗号化 secret のみ
- detail 画面で password rotate を将来的に提供できる余地を残す

## 9. Networking / ingress / Traefik 連携

### 9.1 デフォルト接続方式
MVP では **host port mapping による native SSH** を標準とする。

- 各 workspace に一意な host port を払い出す
- detail 画面に `ssh -p <port> codespace@<host>` を表示する
- port 衝突は `port_leases` で管理する

### 9.2 optional Traefik label 生成
Traefik が既に外部に存在する環境向けに、workspace container へ label を自動付与できる設計を入れる。

例:
- `traefik.enable=true`
- `traefik.http.routers.ws-<id>.rule=Host(...)`
- `traefik.http.services.ws-<id>.loadbalancer.server.port=<internal-port>`

これにより browser access や HTTP preview は `https://<random-name>.<ip>.sslip.io` 的な名前へ載せやすくなる。

### 9.3 443 共有 SSH の調査結果と方針
**raw SSH を hostname ベースで 443/TCP に多重化することは、そのままでは不可** とみなす。

理由:
- SSH は HTTP Host header や TLS SNI を持たない
- Traefik の HostSNI ベース TCP routing は TLS/SNI 前提
- したがって plain SSH を `workspace-a.example` / `workspace-b.example` のように 1 ポートで振り分けられない

可能な代替案:
1. **TLS wrapping + SNI routing**  
   `stunnel` / `openssl s_client` / `wstunnel` などを使い、SSH を TLS で包んでから Traefik で振り分ける。
2. **SSH over WebSocket / HTTPS**  
   `wss://` 経由で tunnel し、クライアント側は `ProxyCommand` や補助 CLI を使う。

結論として、初期実装では以下の優先順位とする。
- まずは native SSH + host port mapping を正式サポート
- Traefik auto-label は HTTP/HTTPS ingress 用に optional 提供
- 443 共有 SSH は **phase-2 research item** として扱い、導入する場合は `wstunnel` 系の wrapper を前提にする

## 10. Web UI 設計

### 10.1 UI stack
- SSR: `templ`
- CSS: Tailwind CSS v4
- JS: Stimulus controller のみ
- リアルタイム更新: SSE 優先、必要に応じて fetch polling fallback

HTMX は使わず、フォーム submit / detail update / log tail は Stimulus から既存 endpoint を叩く形にする。

### 10.2 画面一覧
1. **workspace list**  
   状態、repo、source 種別、TTL、SSH 接続情報、job 状況を表示。
2. **workspace detail**  
   SSH コマンド、複数鍵の要約、password reveal、proxy、resource 制限、event log、削除アクションを表示。
3. **workspace create**  
   built-in image / image ref / Dockerfile / Nixpacks の選択、repo URL、dotfiles、複数 SSH 鍵、password login、proxy、CPU/Memory、TTL を入力。
4. **job list**  
   job type、workspace、状態、開始時刻、実行時間、最新 log を表示。
5. **job detail**  
   append-only log、停止/再開/削除ボタン、関連 workspace へのリンクを表示。

### 10.3 Tailwind v4 の扱い
Tailwind v4 は公式 CLI で build し、生成済み CSS を app image に同梱する。これは UI build tool であり、別のフロントエンドアプリを持つわけではない。

## 11. API / ハンドラ設計

### 11.1 主要エンドポイント案
- `GET /workspaces`
- `GET /workspaces/new`
- `POST /workspaces`
- `GET /workspaces/{id}`
- `POST /workspaces/{id}/delete`
- `POST /workspaces/{id}/password/reset`
- `GET /jobs`
- `GET /jobs/{id}`
- `GET /jobs/{id}/logs/stream`
- `POST /jobs/{id}/cancel`
- `POST /jobs/{id}/pause`
- `POST /jobs/{id}/resume`
- `POST /jobs/{id}/delete`
- `GET /healthz`

### 11.2 ハンドラ原則
- 入力検証は HTTP 層でなく domain/service 層に集約する
- runtime/build エラーは握りつぶさず job log と event log に残す
- SSE endpoint は read-only にし、状態変更は必ず command endpoint 経由にする
- password reveal は one-time token 付き endpoint に限定する

## 12. テスト戦略

### 12.1 Go テスト
- domain/state machine の unit test
- repository / migration の integration test
- runtime abstraction の mock test

### 12.2 Playwright smoke E2E
初期の E2E は smoke test に限定する。

最低限のシナリオ:
1. Cheapspace を起動する
2. workspace list 画面が表示される
3. create form が開く
4. built-in image を選んで作成要求を送れる
5. job list と job detail が表示される

CI 安定性のため、**Playwright はまず mock runtime backend を使って動かす**。その後、実 runtime を使う manual / nightly smoke を追加できるようにする。

## 13. GitHub Actions / 配布

### 13.1 Cheapspace app CI
- `go test ./...`
- `go vet ./...`
- `templ generate` / `sqlc generate` / `goose status` の整合確認
- Tailwind asset build
- Playwright smoke E2E
- Cheapspace app image build

### 13.2 workspace image publish
- 毎週 schedule trigger
- Docker Hub へ push
- タグ: `latest`, `YYYY`, `YYYYMM`, `YYYYMMDD`
- 手動 dispatch も許可

### 13.3 app image publish
- main branch push / tag で app image を build/push
- version tag, git sha, `latest` を付与

## 14. セキュリティと運用前提
- Cheapspace 本体は `/var/run/docker.sock` を扱うため、**信頼済み管理者向け control plane** として運用する。
- workspace には host socket を見せず、rootless nested runtime を使う。
- `--privileged` は採用せず、必要権限は検証で最小化する。
- password login は平文保持しない。
- 認証は外部 proxy に委譲する前提なので、public exposure 時は reverse proxy / auth proxy 配下で運用する。
- Podman 対応は Docker-compatible API に寄せるが、build 機能や label 挙動の差分は adapter 層に閉じ込める。

## 15. 実装順序
1. Go アプリ骨格、`serve` / `migrate` subcommand、単一サービス compose、設定読み込み
2. `goose` migration と `sqlc` 基盤、workspace/job/access スキーマ
3. runtime abstraction と mock backend
4. job runner、job log 保存、job list/detail UI
5. built-in workspace image と rootless DinD 検証
6. build source（image ref / Dockerfile / Nixpacks）統合
7. workspace list/detail/create/delete UI と password/SSH key handling
8. optional Traefik label 生成
9. Playwright smoke E2E と CI/CD
10. 運用ドキュメント整備

## 16. 最終提案
Cheapspace の初期実装は **Go 単一バイナリのモノリス + templ SSR + Tailwind v4 + Stimulus + SQLite(sqlc/goose) + Docker-compatible runtime abstraction** を推奨する。これにより、「単一言語志向」「self-hostable」「`docker compose up` で最小構成」「workspace に DinD を提供」「job 可視化」「将来的な Podman / Traefik 連携」という要求を同時に満たしやすくなる。

特に重要なのは、**native SSH はまず host port mapping で確実に提供し、443 共有 SSH は tunnel 技術を前提に phase-2 として切り出す** こと、そして **workspace には host docker.sock を渡さず rootless nested runtime を使う** ことである。これにより、MVP を現実的な複雑さに保ちながら、将来拡張の余地も確保できる。
