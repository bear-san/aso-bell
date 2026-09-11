# 11. スケジューラ / ジョブワーカー設計

## 1. 目的

リマインド投稿、チラ見の期限切れ、イベントの自動終了、チャンネルアーカイブといった「未来に実行する処理」を、プロセス再起動をまたいでも失わずに実行する。

## 2. 方式

cron ライブラリによるインメモリスケジュールではなく、**MongoDB の `jobs` コレクションを永続キューとして用いるポーリングワーカー**を採用する。

理由:
- プロセス再起動やデプロイでスケジュールが消えない(NFR-03)
- イベント編集時の「該当ジョブだけ取り消して再生成」が DB 操作で完結する
- 複数プロセスで動かしてもリース(`leaseUntil`)により二重実行を防げる
- 依存ライブラリが増えない

## 3. ワーカーループ

```text
loop every pollInterval (default 10s):
  for i in 0..batchSize (default 10):
    job := claim()            // FindOneAndUpdate
    if job == nil: break
    go run(job)
```

### 3.1 claim

```text
filter: {
  $or: [
    { status: "pending", runAt: { $lte: now } },
    { status: "running", leaseUntil: { $lte: now } }   // リース切れ = 前回実行中にクラッシュ
  ]
}
update: {
  $set: { status: "running", leaseUntil: now + leaseDuration (2m), updatedAt: now },
  $inc: { attempts: 1 }
}
sort: { runAt: 1 }
returnDocument: After
```

単一ドキュメントの原子的更新であるため、複数ワーカーが同じジョブを取得することはない。

### 3.2 run

1. `attempts > maxAttempts` なら `failed` にして終了
2. kind に対応するハンドラを呼ぶ(ハンドラは冪等であること)
3. 成功: `status: "done"`, `finishedAt: now`
4. 失敗(リトライ可能): `status: "pending"`, `runAt: now + backoff(attempts)`, `lastError`。backoff は `30s * 2^(attempts-1)`、上限 30 分
5. 失敗(リトライ不能 `ErrPermanent`): `status: "failed"`, `finishedAt: now`

Provider が offline(gRPC `UNAVAILABLE`)の場合はリトライ可能エラーとして扱う。復帰後に backoff の次回時刻で実行される。リマインドはイベント開始を過ぎたら送る意味がないため、`reminder` ハンドラは `startsAt` を過ぎていれば no-op で成功扱いにする。

実行中はリースを更新するハートビートを 1 分ごとに行う(長時間実行への備え。v1 のジョブはすべて数秒で終わる想定)。

### 3.3 cancel

`CancelByEvent(eventId, kinds...)`: `{ eventId, kind: { $in }, status: "pending" }` を `canceled` に一括更新する。`running` のものは実行完了を待つ(ハンドラ側でイベント状態を再確認して no-op にする)。

## 4. ジョブハンドラ

すべてのハンドラは実行時点の DB 状態を再読込し、前提が崩れていれば何もしないで成功扱いにする。

### 4.1 `reminder`

1. Event を取得。`status != open` なら no-op
2. `sequence` が `reminder_logs` に存在すれば no-op(二重送信防止)
3. アクティブ参加者(`participant/active`)を取得
4. イベントチャンネルへリマインドを投稿(参加者メンション付き)
5. 募集チャンネルが設定されていれば、ボタン付きリマインドを投稿
6. `reminder_logs` へ結果を記録。片方のみ失敗した場合もログに残し、ジョブは成功扱い(部分失敗の再送はしない)

### 4.2 `peek_expire`

1. Participation を取得。`role != peeker || status != active` なら no-op
2. `expiresAt > now` なら(延長された場合)`runAt = expiresAt` で再スケジュール
3. Provider RPC で除外。`was_member: false` は成功扱い。Provider offline(`UNAVAILABLE`)はリトライ
4. `status: "expired"` に更新

### 4.3 `event_auto_end`

1. Event を取得。`status != open` なら no-op
2. 終了予定時刻(`endsAt` または `startsAt + autoEndGrace`)を再計算し、まだ未来なら再スケジュール(編集で延期された場合)
3. Manager の `EndEvent(reason: auto)` を呼ぶ

### 4.4 `archive_channel`

1. Event を取得。`channel.archived` なら no-op
2. Provider の `ArchiveChannel` を呼ぶ
3. `channel.archived = true`、Discord の場合は `channel.name` も更新

## 5. ジョブ生成規則

| タイミング | 生成されるジョブ | dedupeKey |
| --- | --- | --- |
| イベント作成 / リマインドポリシー変更 / 日時変更 | `reminder` × N | `reminder:<eventId>:<sequence>` |
| イベント作成 / 日時変更 | `event_auto_end` | `auto_end:<eventId>` |
| チラ見 | `peek_expire` | `peek:<participationId>:<peekCount>` |
| 終了 / 中止 | `archive_channel` | `archive:<eventId>` |

ポリシー変更時は既存の `pending` な `reminder` を `CancelByEvent` してから再生成する。再生成後の `sequence` は 1 から振り直すが、`reminder_logs` の重複判定は `(eventId, scheduledFor)` の時刻一致で行い、既送信時刻と同じ時刻のリマインドは送らない。

### 5.1 リマインド時刻の計算

```text
offsets モード:
  for off in offsets: t = startsAt - off; if t > now: emit(t)

interval モード:
  tz := workspace.timezone
  first := 次に来る tz の at 時刻(now 以降)
  for t := first; t < startsAt - finalOffset; t += every: emit(t)
  emit(startsAt - finalOffset) if > now
```

emit した時刻を昇順にソートし、`sequence` を 1 から振る。30 件を超える場合はポリシー保存時に拒否する。

## 6. 設定

| 環境変数 | 既定 | 説明 |
| --- | --- | --- |
| `ASOBELL_JOBS_POLL_INTERVAL` | `10s` | ポーリング間隔 |
| `ASOBELL_JOBS_BATCH_SIZE` | `10` | 1 回のポーリングで取得する最大数 |
| `ASOBELL_JOBS_LEASE` | `2m` | リース期間 |
| `ASOBELL_JOBS_WORKERS` | `4` | 同時実行数 |

## 7. 可観測性

- ジョブの開始・完了・失敗を構造化ログに出力(`job_id`, `kind`, `event_id`, `attempt`, `duration_ms`)
- WebConsole のイベント詳細で、該当イベントの `jobs` を表示する(予定リマインド一覧として)

## 8. テスト

- `claim` の排他性: 同一ジョブに対して並列に claim を呼び、1 回だけ取得できることを testcontainers の MongoDB で検証
- リース切れの再取得
- backoff 計算、maxAttempts 到達で `failed`
- 各ハンドラの no-op 条件(状態が変わっていた場合)を Fake ProviderPort + Fake Clock で検証
- リマインド時刻計算はテーブル駆動テスト(タイムゾーン、DST なし地域と DST あり地域の両方)
