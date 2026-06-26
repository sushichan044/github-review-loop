# github review loop — SPEC

> Status: WHY / コアモデル / config / 出力 / アーキテクチャは確定。末尾「未確定」は実装着手時に詰める。
> このドキュメントは grilling セッションで詰めた設計判断の確定版。

---

## WHY

このツールは **AI レビューの自動化ではない**。コメントを読んで直して push するのは引き続き AI / 人間が行う。

このツールが消すのは **review loop の *機構* の不確実さ**に限定する。

1. push 前に再依頼してしまう → ツールが構造的に防ぐ
2. 何回ラリーしたか数えられず無限化する → event history から再構成する
3. 再依頼を忘れる / 止め時を見失う → status 出力が指摘する

### 非ゴール

「コメントが本当に妥当に対応されたか」の品質保証はしない。

AI review は的を射ていないこともあり、**対応しない理由をコメントして resolve する**ことはまともなコードレビューのあり方である。
よってツールは中身の妥当性を裁かず、**loop 機構の記帳に徹する**。

---

## コアモデル

**reviewer ごとに独立した state machine を持ち、各々が `goal達成` か `exhausted` に遷移するまで loop を回すツール。**
loop 全体は、全 state machine が終端 (`goal達成` または `exhausted`) に達したら停止する。

状態は PR の GitHub timeline (event history) から **stateless に再構成**する。永続的な状態ファイルは持たない。

| 項目 | 定義 |
|---|---|
| **実質ゴール (goal)** | `approved`（approve 可能な reviewer）／ `all-conversations-resolved`（approve 不可能な reviewer）。reviewer ごとに config で指定 |
| **max-rallies** | 実質ゴールと **直交する安全弁**。既定 5。枯渇 (`exhausted`) ＝ goal 未達のまま打ち切り、status で警告表示する |
| **1 rally** | **「トリガー行為」の数**（発火手段に依存しない統一定義）。review-request 型は request イベント数、comment 型はトリガーコメント数。初回トリガーも 1 rally に含む |
| **unresolved** | **review conversation thread のみ**を対象（issue comment は除外）。`isResolved` で判定し、**誰が resolve したかは問わない** |
| **再依頼ガード** | その reviewer の最後の review 以降に head が新規 commit で進んでいなければ、再依頼を **no-op で拒否**（理由を出力）。WHY #1 を構造的に潰す |
| **再依頼の主導** | タイミング判断は **AI**（明示的に再依頼サブコマンドを叩く）。ツールはガードを通過した場合のみ API を発火する |
| **トリガー戦略** | reviewer type ごとに発火方法を持つ。**全 type に default を提供**し、**github-app だけ go template 等で上書き可能**（comment or command） |
| **loop 全体の終了** | 1 人でも（枯渇以外で）goal 未達なら継続。全 reviewer が `goal達成` または `exhausted` になったら終了 |
| **status 出力** | 前回 rally 以降の差分コメント＋現在 unresolved なコメント＋AI 向けの「unresolved を resolve させる instructions」。next-action は常時出力 |

### state machine 遷移

```
                  goal 達成
   [active] ───────────────────▶ [goal達成] (終端)
      │
      │ rally 数 >= max-rallies かつ goal 未達
      └───────────────────────▶ [exhausted] (終端 / 警告)
```

- `active` で再依頼するたびに rally がインクリメントされる（ガード通過時のみ）。
- rally 数は timeline から再構成するため、ツール外（GitHub UI からの手動再依頼、CODEOWNERS 自動アサイン等）で発生したトリガー行為もカウント対象になる。

---

## config

- 設定は **適用先のリポジトリ内**に置く。パスは repo ルートの `.github/review-loop.yml`（`.yaml` も許容）。探索は working directory から `.git` を持つ親へ遡って repo ルートを特定し、その `.github/` を読む。
- **マシングローバルな設定は持たない。** これは意図的な決定。レビュー方針のゴールは「チーム/組織の全員と CI に等しく効かせる」こと。`~/.config` のような per-machine ファイルは MDM 等で全員に配らない限り全員には届かず、CI にも届かない。全開発者と CI に確実に届く唯一の場所が **repo に commit されたファイル**。したがって配布・強制の機構として global config は不適格で、廃止した。
- top-level key は **`reviewers`** のフラットなリスト。1 repo = 1 方針なので scope / owner / repo といったキーは持たない（owner/repo は git remote / PR から解決する config の責務外）。
- `max-rallies` は goal から切り出した **reviewer 直下の独立フィールド**（省略時は default 5）。
- 設定が無い repo では `status` / `request` はエラーにせず no-op し、`init` を促すヒントを出す。`init` は commented テンプレートを `.github/review-loop.yml` に書き出す（既存ファイルは上書きしない）。テンプレートは `//go:embed` で実ファイルとして同梱。
- GitHub token は `github.com/cli/go-gh/v2/pkg/api`（`api.DefaultRESTClient()` / `api.DefaultGraphQLClient()`）が gh の認証（`gh auth token` と同ソース、`GH_TOKEN` フォールバック）から自動取得。明示管理しない。

```yaml
# .github/review-loop.yml
reviewers:
  - type: user
    name: sushichan044
    goal: { approved: true }          # 実質ゴール
    max-rallies: 5                     # 直交する安全弁（省略時 default）
  - type: github-copilot               # approve 不可能なので resolved がゴール
    goal: { all-conversations-resolved: true }
    max-rallies: 5
  - type: github-app
    name: coderabbitai
    goal: { approved: true }
    max-rallies: 5
    trigger: "@coderabbitai review"    # github-app のみ default を上書き
```

> 補足: 「config が repo にある」ことと「enforcement が達成される」ことは別。実際に効かせるには CI / エージェントが `github-review-loop` を起動する配線が要る。また PR で config 自体を弱められないようにするなら `.github/review-loop.yml` に CODEOWNERS をかける。いずれも本ツールのスコープ外。

---

## 出力 (agentdetection)

- フラグ `--format ("agent" | "human")`、**default `human`**。
- **agentdetection は default 値を `agent` に倒すだけ**。明示された `--format` フラグが常に優先される。
- agentdetection は **出力表現のみ**を切り替える。状態計算・ガード・API 発火などの **振る舞いは一切変えない**。
- **next-action guidance（今何が起きている / 次に何をすべきか）は format に関わらず常時出力**。format は markdown 化・詳細度（冗長度）だけを切り替える。

この設計により、agentdetection の false negative が起きても AI 向けの next-action は失われず、loop が静かに劣化しない。

---

## アーキテクチャ

将来他 VCS にも対応できるよう、GitHub に強く依存する層とコアを分離する。

- **コア層（VCS 非依存）**: state machine、rally / goal / ガード / loop 終了の判定。入力は抽象化した event 列。
- **GitHub 層**: timeline fetch（`gh-timeline` の fetch 手法を流用）、reviewer type 別のトリガー戦略、unresolved thread 取得、`go-gh` 呼び出し。
- 将来 VCS 追加時はコア層を再利用する。

### 先行事例

[`k1LoW/gh-copilot-review`](https://github.com/k1LoW/gh-copilot-review) は **単一 reviewer（copilot）の 1 rally** に相当する。
本ツールはこれを **N reviewer × goal × max-rallies の stateless loop** に一般化したもの。

- 「already reviewed the current head commit → exit early」は本ツールの **再依頼ガード**そのもの。
- old review の minimize、`--wait` polling、unresolved inline comment 数の報告も参考になる。

---

## 未確定（実装着手時に詰める / WHY・SPEC には影響しない）

1. **対象 PR の解決**: go-git で現在 repo を取得し current branch の PR を引く想定。branch に複数 PR がある場合 / PR が無い場合のフォールバックを定義する。
2. **comment-trigger bot の unresolved 判定**: coderabbit のインラインコメントが review conversation thread として取得できるか実機確認する（「review conversation のみ」で動く想定）。
