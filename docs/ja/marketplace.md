---
layout: default
lang: ja
ref: marketplace
traduit_le: 2026-11-25
---
# pi-appliance の marketplace

marketplace には、pi-appliance のコンソールからインストールして管理できるアプリケーションが集まっています。カタログはこのサイトで公開されています。インストール済みのコンソールは 6 時間ごとに読み直し、承認されたアプリケーションは pi-appliance の新しいバージョンなしでそこに表示されます。

> **実験版です。** イメージは Raspberry Pi 3 B+ と Raspberry Pi 4 で検証済みです。変更のたびに自動テストが各アプリケーションをインストールし、自宅とインターネットからそのページを開きますが、各アプリケーションは実際の Pi でも試す必要があります。サービスはインターネットからも Pi のアドレスの下で開けます（[ガイド（フランス語）]({{ '/guide-utilisateur.html#services-sur-internet' | relative_url }})を参照）。

## pi-appliance の基盤

**pi-appliance の基盤**（Docker、Traefik、システムのサービスとツール、コンソール、`pia` コマンド）はイメージに含まれています。カタログにはなく、アンインストールもできません。以下のアプリケーションをコンソールからインストールして管理できるのは管理者だけです（初回起動時に作成したアカウント、または別の管理者アカウント）。

## カタログにあるアプリケーション

<div id="marketplace-catalog" data-catalog-index="{{ '/static/pi-appliance-catalog.json' | relative_url }}">
<p id="marketplace-catalog-status" role="status">カタログを読み込んでいます…</p>
<p id="marketplace-tags-help" hidden>タグをひとつ以上タップすると、そのいずれかを持つアプリケーションだけが表示されます。もう一度タップすると解除されます。</p>
<div id="marketplace-tags" class="todo-filters" role="group" aria-label="タグで絞り込む" hidden></div>
<p id="marketplace-tags-empty" hidden>これらのタグを持つアプリケーションはありません。</p>
<table id="marketplace-catalog-table" hidden>
<thead><tr><th>アプリケーション</th><th>説明</th><th>カテゴリ</th><th>タグ</th><th>メモリ</th><th>ディスク</th></tr></thead>
<tbody></tbody>
</table>
</div>

アプリケーションの名前と説明はそれぞれのマニフェストから取得しており、今のところフランス語のみです。

この一覧は、[`marketplace/`](https://github.com/gmoulard/pi-appliance/tree/main/marketplace) のマニフェストから生成された JSON カタログから読み込まれます。コンソールも GitHub リポジトリにある同じファイルを読みます。承認されたアプリケーションは、pi-appliance の新しいバージョンなしで Pi に表示されます。マニフェストや `marketplace/planned.yaml` を変更したら、リポジトリのルートで `go run ./cmd/marketplace-json` を実行してファイルを再生成してください。そうしないとテスト（`go test ./...`）が失敗します。忘れた場合は、承認後に GitHub のアクションが `main` 上で再生成します。ファイル名には内容の MD5 が含まれ、インデックス `docs/static/pi-appliance-catalog.json` が現在のバージョンを示します。

コンソールは複数のインスタンス、その起動と停止、元に戻せる更新、そしてアンインストール時のデータの保持または削除を管理します。

## 予定・検討中のもの

これらのアプリケーションは、まだ marketplace からインストールできません。一覧は同じ JSON カタログ（`marketplace/planned.yaml`）から取得しています。

<ul id="marketplace-planned" hidden></ul>

**コンソールに組み込み、カタログ外**：[Cockpit](https://cockpit-project.org/)。Pi のシステム管理（ログ、プログラム、ネットワーク、ディスク、ブラウザー上のターミナル。上級者向け）です。コンテナではなくシステムのソフトウェアで、コンソールの Marketplace が自宅でのみ管理者に提供し、別のパスワードなしで pi-appliance のアカウントで入れます（[仕様]({{ '/specification.html' | relative_url }})（フランス語）の 6.4 節を参照）。

**カタログ外：自由な Docker Compose**：管理者は、カタログにないアプリケーションも、その Docker Compose ファイルから（貼り付けるか、GitHub のページなどの Web アドレスから読み込んで）インストールできます。このサービスはプロジェクトによる確認もサポートもありません（[ガイド（フランス語）]({{ '/guide-utilisateur.html#docker-compose-libre' | relative_url }})を参照）。

データベース：PostgreSQL を Web インターフェース Adminer とともに利用できます。MongoDB（Pi 5 のみ）、Redis、MSSQL（Pi 用のイメージなし）、SQLite（単なるファイル）は単独では提供しません。[仕様]({{ '/specification.html' | relative_url }})（フランス語）の 6.3 節を参照してください。

Etherpad の公開共有リンクも予定している改善です。進捗は[ロードマップ]({{ '/TODO.html' | relative_url }})（フランス語）をご覧ください。

## アプリケーションの追加：誰でも貢献できます

貢献を歓迎します。自宅で役立つ自由なアプリケーションの提案（現時点で唯一の例外は n8n。*fair-code* の「Sustainable Use License」で、プロジェクトがあえて採用し、紹介ページに明記しています）、既存のマニフェストの改善、問題の報告ができます。提案は pull request で行い、取り込む前にレビューとテストを行います。

### 1. Docker Compose のマニフェストを作る

[pi-appliance リポジトリ](https://github.com/gmoulard/pi-appliance)を fork し、`marketplace/<識別子>/compose.yaml` を作成します。識別子は小文字で、英字で始まり、英字・数字・ハイフンだけを含む必要があります。

このファイルは、アプリケーションを説明する `x-pi-appliance` セクションを持つ通常の Docker Compose マニフェストです。構成の例（値とイメージはご自分のアプリケーションのものに置き換えてください）：

```yaml
x-pi-appliance:
  name: マイアプリ
  description: 利用者向けのわかりやすい説明。
  icon: M4 5h16v14H4z
  category: Outils
  tags: [administration]
  license: AGPL-3.0
  subdomain: outil
  service: app
  port: 8080
  memory_mb: 256
  disk_mb: 512
  login: application
  access: users
  parameters:
    - name: ADMIN_PASSWORD
      label: 管理者パスワード
      generate: true

services:
  app:
    image: editeur/image:1.2.3
    restart: unless-stopped
    environment:
      ADMIN_PASSWORD: ${ADMIN_PASSWORD}
    volumes:
      - data:/var/lib/application

volumes:
  data:
```

### 2. カタログのルールを守る

- **正確なバージョン**（`latest` や `stable` ではない）を持ち、対象の Raspberry Pi のアーキテクチャである `linux/arm64` に対応した Docker イメージを選んでください。
- 公開するサービスを `service` に、その内部ポートを `port` に、そして `memory_mb` と `disk_mb` には MB 単位の現実的な見積もりを記入してください。
- `icon` フィールドは 24 × 24 のグリッド上の SVG パス（`d`）です。`login` は `application` または `console`、`access` は `public`、`users`、`home`、`admins`（管理者・自宅のみ。Portainer や Claude Code のように Pi 全体を操作できるアプリケーション用）を受け付けます。
- パラメーターは `parameters` に大文字で記入してください。秘密の値には、マニフェストやイメージにパスワードを書く代わりに `generate: true` を使ってください。
- 永続データは名前付きの Docker ボリュームに保存してください。秘密の値、個人データ、利用者固有の設定をリポジトリに入れないでください。決してバックアップしてはいけないボリューム（アカウントのトークン）には `pi-appliance.backup: "false"` ラベルを付けます（ボリュームの `labels`。Claude Code と同様）。
- アプリケーションのライセンスを記入し、その再配布がプロジェクトと両立することを確認してください。
- `x-pi-appliance` では文書化されたキーだけを使ってください。未知のキーがあるとマニフェストは拒否されます。
- Pi 全体を操作できるオプション（特権コンテナ、Docker へのアクセス、Pi のフォルダー、ホストのネットワーク、追加の権限）は避けてください。これらを持つ新しいアプリケーションはオンラインのカタログからはインストールできず、pi-appliance の新しいバージョンの後でのみ可能になります。Docker にアクセスする、または特権コンテナであるアプリケーションは、さらに `access: admins` を宣言する必要があり、コンソールはインストール前に確認を求めます。
- 予定しているがまだ準備ができていないアプリケーションは `marketplace/planned.yaml` に入れます（名前、説明、アイコン、カテゴリ、ロードマップの行）。サイトでは「近日公開」と表示されます。

フィールドの完全な定義と検証ルールは[仕様の 6.5 節]({{ '/specification.html' | relative_url }})（フランス語）にあります。

### 3. 確認して提案する

リポジトリのルートで次を実行します。

```shell
go run ./cmd/marketplace-json
go test ./...
```

テストでは主に、マニフェストが有効か、イメージのバージョンが固定されているか、Compose の変数が宣言されているかを確認します。可能なら、テスト用のマシンで Docker を使い、アプリケーションのインストール、停止、再起動、アンインストールも試してください。

その後、アプリケーションの説明、必要なリソース、テスト手順、既知の制限を添えて `main` への pull request を作成してください。pull request が `main` で承認されると、各アプライアンスは pi-appliance の新しいバージョンなしで、6 時間以内に新しいオンラインのカタログを読み込みます。

## 便利なリンク

- [marketplace のソースコード](https://github.com/gmoulard/pi-appliance/tree/main/marketplace)
- [マニフェストの仕様]({{ '/specification.html' | relative_url }})（フランス語）
- [ロードマップ]({{ '/TODO.html' | relative_url }})（フランス語）
- [pi-appliance リポジトリ](https://github.com/gmoulard/pi-appliance)：fork から pull request を作成して、貢献を提案してください。
