# ADR 0008: Manager と Provider を別コンテナに分離し、相互に gRPC サーバーを持つ

- 状態: 採用(ADR 0001 の「Bot も同一プロセス」を置き換える。2026-09-11 に Manager 1 : Provider 1 へ改訂)
- 日付: 2026-09-11

## 文脈

要件変更により、Manager と Provider(チャットツール接続)を別コンテナで稼働させ、gRPC で通信することになった。**1 つの Manager は 1 つの Provider にのみ接続する**。1 グループは 1 つのチャットツールだけを使い、複数グループに提供する場合はグループごとに Manager + Provider のペアを用意する。Provider の資格情報は環境変数で与える。

通信方向は双方向に必要である。Provider → Manager(コマンド・ボタン・フォーム・ワークスペース通知)と Manager → Provider(状態取得・チャンネル作成・投稿・ピン等)。実現方式は 2 つ:

- (a) 両側が gRPC サーバーを持ち、相互に dial する
- (b) Manager だけがサーバーを持ち、Provider が張る長寿命の双方向ストリームに Manager → Provider の要求を相関 ID 付きで流す

## 決定

(a) を採用する。Manager と Provider は互いのアドレスを環境変数(`ASOBELL_PROVIDER_ADDR` / `ASOBELL_MANAGER_ADDR`)で静的に設定し、自己登録・ハートビート・サービスディスカバリは設けない。Manager は `GetInfo` を定期的に呼んで Provider の状態を監視し、初回接続時に種別を DB に記録して以後の起動時に照合する。

## 理由

- 各方向が通常の unary RPC になり、デッドライン・ステータスコード・リトライポリシー・Health・Reflection といった gRPC 標準機能をそのまま使える
- (b) は逆向き RPC の相関・タイムアウト・再接続を自前実装する必要があり、ストリーム断の扱いが複雑になる
- 想定配置は Docker Compose の同一ネットワークで、Provider への到達性は問題にならない
- 1:1 に限定されたため、Provider の登録・発見・複数接続の管理は不要。静的なアドレス設定が最も単純で、設定ミスも起動時の疎通確認で即座に分かる
- 種別の照合により、Slack 用の DB に Discord Provider を誤って接続してデータを壊す事故を防ぐ

## 結果

- 複数グループに提供する場合は Compose プロジェクトを分けてセットを複数起動する。グループ間でデータは共有されない
- 1 グループで Slack と Discord を併用する要件が出た場合は、Manager 側で複数 Provider を扱う設計(登録・Workspace ごとの振り分け)に拡張する必要がある
- Provider を NAT 越しに配置する要件が出た場合は、(b) を Provider → Manager の通知ストリームに限定して併用するハイブリッドを検討する
- 両側に共有トークン認証(と任意の mTLS)が必要
- 依存: `google.golang.org/grpc`(Apache-2.0)、`google.golang.org/protobuf`(BSD-3、回避不能のため ADR 0002 の例外に追加)、`bufbuild/buf`(Apache-2.0)
