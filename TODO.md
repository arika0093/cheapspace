* レポジトリ+ブランチ指定できるようにする
* CLIツールを用意。webからの操作以外に、CLIからも操作できるようにする。
  * 起動中のcodespace一覧(list)。
  * 詳細閲覧: SSHのコマンド、環境詳細、etc.(view)
  * 新規立ち上げ(new)
  * 削除(delete)


将来的には::
* GitHub App作成。
  * PR作成時に自動でcodespaceを立ち上げる。
  * PRクローズ時に自動でcodespaceを削除する。
  * PRコメントでcodespaceを立ち上げる、削除する、作り直す操作をできるようにする。
* Gitea, GitLab, Bitbucket等の他のGitホスティングサービスでも同様の機能を提供できるようにする。