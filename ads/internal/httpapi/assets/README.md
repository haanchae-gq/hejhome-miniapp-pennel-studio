# 벤더된 디자인 시스템 산출물

`tokens.css` 는 **헤이홈 디자인 시스템(`design-guide`)의 `dist/tokens.css` 사본**이다.
직접 고치지 마라 — 원천은 `design-guide/tokens/*.json` 이고 거기서 재생성된다.

## 왜 사본인가

스튜디오(Node)는 `src/hej.mjs` 가 `../design-guide/dist/` 를 **런타임에 읽는다**.
광고 서버는 Go 단일 바이너리로 **별도 배포**되므로 그 경로가 없다. 그래서 빌드 시점에
`go:embed` 로 굽는다.

사본은 낡는다. 그래서 갱신을 명령 하나로 만들어 둔다:

```bash
npm run ads:tokens      # design-guide/dist/tokens.css → 여기로 복사
```

## 콘솔이 지켜야 할 것

**자체 팔레트를 만들지 않는다.** 색은 전부 `--color-*` 시맨틱 토큰에서 온다.
`#00A872` 같은 하드코딩 hex 를 콘솔 CSS 에 쓰지 마라 — 다크 모드가 깨지고,
디자인 시스템이 바뀌어도 따라가지 못한다.
