# ADR 0005: Discord のプライベートチャンネルは permission overwrite で実現する

- 状態: 採用
- 日付: 2026-09-11

## 文脈

Discord にはプライベートチャンネルの概念が無く、`@everyone` の `VIEW_CHANNEL` を拒否し、閲覧を許可する対象を overwrite で列挙して実現する。対象はユーザー単位(Member overwrite)か、イベントごとのロール(Role overwrite + ロール付与)のいずれか。

## 決定

v1 はユーザー単位の Member overwrite を採用する。

## 理由

- 「チャンネルに追加 / 除外」という要件の操作が `ChannelPermissionSet` / `ChannelPermissionDelete` の 1 呼び出しに対応し、状態が単純
- ロール方式はイベントごとにロールを作成・削除する必要があり、ギルドのロール上限(250)とロール管理権限の扱いが増える

## 結果

- overwrite 数の上限(公式未記載、サポート情報でギルド全体 1000 程度)により、大人数イベントには不向き。参加者 50 名超で WebConsole に警告を出す
- 将来ロール方式へ切り替える場合は `provider/discord` 内の `AddMember` / `RemoveMember` / `CreatePrivateChannel` の変更で済む
