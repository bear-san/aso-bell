# ADR 0001: Manager を単一プロセスのモジュラーモノリスとして構成する

- 状態: 一部改訂(ADR 0008 により Bot の Provider 部分は別コンテナへ分離。Manager 内部の構成としては有効)
- 日付: 2026-09-11

## 文脈

要件は Bot / Manager / WebConsole / DB の 4 コンポーネントを挙げ、Manager は「API 経由でライフサイクルを管理」するとしている。想定利用規模は個人〜小規模コミュニティ。

## 決定

Manager(ユースケース層 + HTTP API + gRPC サーバー)、WebConsole(静的配信)、Job Worker、Bot Core(コマンド解釈)を 1 つの Go プロセスで動かす。Job Worker と Bot Core は Manager のユースケースを Go インターフェース経由で直接呼び、HTTP API と gRPC は同じユースケースの外皮とする。チャットツールへの接続(Provider)は別コンテナ(→ ADR 0008)。

## 理由

- Manager の運用対象が 1 バイナリ + MongoDB に収まり、Docker Compose で完結する
- Bot Core → ユースケースの呼び出しがプロセス内で完結し、Provider との往復は 1 回で済む
- パッケージ境界(`internal/manager` はインターフェースにのみ依存)を守れば、後から Manager の HTTP クライアント実装を追加して分離できる

## 結果

- 水平スケールは v1 では非対応。ジョブの claim はリース方式で複数プロセスに耐えるよう設計しておく
- `internal/app` が唯一の組み立て点となり、依存の方向を lint(`depguard`)で強制する
