# Cheapspace

GitHub Codespacesの代替

## 機能概要

* Webアプリ。
  * 主な機能
    * 起動中のcodespace一覧(list)。
    * 詳細ページ:SSHのコマンド、環境詳細、etc.(view) 
    * 新規立ち上げ(new)
    * 削除(delete)
  * 将来的にやってもよい
    * WebIDE対応。VSCode Web, Jetbrains IDE等。
  * スコープ外
    * ユーザー/Group管理(認証、認可) -> Autheria等と組み合わせる。
    * 監査ログ → 対応しない。
* 新規立ち上げ時の引数
  * ベースdockerイメージ or Dockerfile. 指定がない場合 devcontainers/universalをベースにしたDockerイメージ（後述）
  * 起動時にgit cloneするリポジトリURL.
  * SSH公開鍵.
  * dotfilesリポジトリURL. 指定がない場合はdotfilesはクローンしない。
  * HTTP(S)_PROXY. 企業環境での使用を想定して、HTTP(S)_PROXY環境変数を受け取る。
  * CPU/Memory制限。起動時間上限 (環境変数で指定されている値以下)。
* 独自dockerイメージ
  * devcontainers/universalをベースにしたイメージを用意。
    * playwrightのインストール(mcpも)
    * @github/copilot, codex, claudecode等のLLM CLIツールインストール。
    * 引数で指定されたリポジトリをクローンして、ワークスペースとする。
    * 専用ユーザー(codespace)を作成。
    * 引数のSSH公開鍵をauthorized_keysに追加。
    * dotfilesリポジトリをクローンして、dotfilesを展開(install.sh等を実行)。
    * 起動スクリプトで以下の内容を立ち上げ
      * tmux
      * sshd
      * dockerd
  * このイメージはGitHub actionsで一週間に一度ビルド、DockerHubにpush。タグはlatest, 年, 年月, 年月日。
* HTTP_Proxyサポート
  * 企業環境での使用をサポート、想定。
  * HTTP(S)_PROXY環境変数を受け取って、codespace内の環境変数としても設定(bashrc等に書き込む)。
  * docker buildの際もこれを利用。
  * WebUIについても同様。
* docker compose upで起動。
  * WebApp(SQLiteボリュームのvolume).
  * 環境引数で 起動WorkspaceのCPU/Memory制限。起動時間上限。
