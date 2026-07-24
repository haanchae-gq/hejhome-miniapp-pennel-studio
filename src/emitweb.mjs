/**
 * emitweb.mjs — 저작 모델 → **웹 랜딩 HTML**.
 *
 * 광고 서버가 서빙할 랜딩 한 장을 만든다. 자립형(외부 요청 0)이라 그대로 올리면 된다.
 *
 * ## 왜 여기(Node)에서 렌더하나
 * 위젯 카탈로그와 스타일 규격(`emitAdStyles`)이 이 리포에 있다. Go 광고 서버로 포팅하면
 * 렌더 규격이 **두 벌**이 되어 갈라진다. 그래서 발행 시점에 여기서 굽고, 광고 서버는
 * 그 HTML 을 **서빙만** 한다. (설계서 §4-1)
 *
 * ## 왜 스튜디오 프리뷰 렌더러를 안 쓰나
 * `web/studio.html` 의 `WIDGETS[].render()` 는 **에디터 캔버스용**이다 — 플레이스홀더
 * 아트, "광고" 아이브로우, 선택 하이라이트 같은 저작 도구의 장치가 섞여 있다.
 * 랜딩은 제품이라 그것들이 나가면 안 된다. 같은 위젯 타입을 쓰되 출력은 다르다.
 *
 * ## 클래스 이름은 패널과 같다
 * `w-point`·`w-fact`·`w-coupon`… 을 그대로 쓰고 `emitAdStyles(panel,{unit:'px'})` 로
 * 스타일을 얻는다. 패널(rpx)과 웹(px)이 **한 규격**에서 나온다.
 */
import { emitAdStyles } from './emit.mjs';

const esc = s => String(s ?? '').replace(/[&<>"']/g, c =>
  ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

/** 링크를 실제 href 로. 없으면 '#'(발행 전 검수에서 걸러야 한다). */
const hrefOf = (model, key) => esc(model.links?.[key]?.url || '#');

/** 위젯 하나 → HTML. 패널 emitter(emitWidget)와 같은 타입 집합을 다룬다. */
function widgetHtml(w, model) {
  const lk = model.links?.[w.link] || {};
  switch (w.type) {
    case 'adHero': {
      const img = lk.image ? `<img class="w-hero-img" src="${esc(lk.image)}" alt="">` : '';
      const sub = lk.desc ? `<p class="w-hero-sub">${esc(lk.desc)}</p>` : '';
      const price = lk.price ? `<p class="w-hero-price">${esc(lk.price)}원<span> 부터</span></p>` : '';
      return `<section class="w-hero">${img}
        <div class="w-hero-body">
          <p class="w-hero-eyebrow">광고</p>
          <h1 class="w-hero-title">${esc(lk.promo || '')}</h1>
          ${sub}${price}
        </div></section>`;
    }
    case 'adBanner':
      return `<a class="w-banner" href="${hrefOf(model, w.link)}">
        <span class="w-banner-label">광고</span>
        <span class="w-banner-title">${esc(lk.promo || lk.desc || '')}</span></a>`;
    case 'adPoint':
      return `<p class="w-point">${esc(w.text || '')}</p>`;
    case 'adFactRow':
      return `<div class="w-fact"><span class="w-fact-label">${esc(w.label || '')}</span>` +
        `<span class="w-fact-value">${esc(w.value || '')}</span></div>`;
    case 'adStepGuide': {
      const items = Array.isArray(w.items) ? w.items : String(w.text || '').split('\n');
      const lis = items.filter(s => String(s).trim()).map((s, i) =>
        `<div class="w-step"><span class="w-step-no">${i + 1}</span><span class="w-step-txt">${esc(s)}</span></div>`).join('');
      return `<div class="w-steps">${lis}</div>`;
    }
    case 'adCoupon':
      return `<div class="w-coupon"><span class="w-coupon-code">${esc(w.text || '')}</span>` +
        `<span class="w-coupon-desc">${esc(w.label || '')}</span></div>`;
    case 'adConsent':
      // 리드수집·체험단은 이 고지 없이 집행 불가다. 비어 있으면 발행 검수가 막는다.
      return `<p class="w-consent">${esc(w.text || '')}</p>`;
    case 'adOfferRow':
      return `<a class="w-offer" href="${hrefOf(model, w.link)}">
        <span class="w-offer-title">${esc(lk.desc || '')}</span>
        <span class="w-offer-reward">${esc(w.text || '')}</span></a>`;
    case 'adProductCard':
      return `<a class="w-product" href="${hrefOf(model, w.link)}">
        <span class="w-product-name">${esc(w.text || lk.desc || '')}</span>
        ${w.price ? `<span class="w-product-price">${esc(w.price)}</span>` : ''}</a>`;
    case 'adDismissBar':
      return `<div class="w-dismiss"><span class="w-dismiss-txt">${esc(w.text || '닫기')}</span></div>`;
    case 'ctaButton':
    case 'linkTile':
      return `<a class="w-cta" href="${hrefOf(model, w.link)}" data-cta="1">${esc(w.text || lk.desc || '자세히 보기')}</a>`;
    default:
      // 기기 위젯(DP 바인딩)은 웹 랜딩에 올 수 없다. 조용히 버리지 않고 주석으로 남긴다.
      return `<!-- 웹 랜딩에서 지원하지 않는 위젯: ${esc(w.type)} -->`;
  }
}

/** 랜딩 전용 추가 스타일 — 위젯 공통 스타일(emitAdStyles) 위에 페이지 골격만 얹는다. */
function pageCss(model) {
  const c = model.theme?.color || {};
  const bg = c.bgHome || '#FFFFFF';
  const surf = c.accentSurface || c.accent || '#00A872';
  const tp = c.textPrimary || '#0F1114';
  const ts = c.textSecondary || '#8B95A1';
  return `
*{box-sizing:border-box}
body{margin:0;background:${bg};color:${tp};
  font-family:-apple-system,BlinkMacSystemFont,"Apple SD Gothic Neo","Noto Sans KR",system-ui,sans-serif;
  -webkit-text-size-adjust:100%}
.wrap{max-width:560px;margin:0 auto;padding:16px 18px 40px}
.w-hero{border-radius:18px;overflow:hidden;box-shadow:0 8px 24px rgba(0,0,0,.10);margin-bottom:18px}
.w-hero-img{width:100%;height:auto;display:block}
.w-hero-body{padding:18px;background:${bg}}
.w-hero-eyebrow{margin:0;font-size:12px;font-weight:700;color:${surf}}
.w-hero-title{margin:6px 0 0;font-size:21px;line-height:1.35}
.w-hero-sub{margin:6px 0 0;font-size:13px;color:${ts}}
.w-hero-price{margin:10px 0 0;font-size:24px;font-weight:700}
.w-hero-price span{font-size:12px;color:${ts};font-weight:500}
.w-banner{display:flex;gap:10px;align-items:center;text-decoration:none;color:inherit;
  border:1px solid ${surf}55;border-radius:14px;padding:12px;margin:10px 0}
.w-banner-label{font-size:11px;color:${ts}}
.w-banner-title{font-size:14px;font-weight:700}
.w-offer,.w-product,.w-cta{text-decoration:none;color:inherit;display:block}
.w-cta{background:${surf};color:#fff;text-align:center;padding:15px;border-radius:14px;
  font-size:16px;font-weight:700;margin-top:18px}
`;
}

/**
 * 저작 모델 → 자립형 랜딩 HTML.
 * @param {object} model  스튜디오 저작 모델(*.studio.json)
 * @param {object} opts   { impParam: true } 면 전환 스크립트를 붙인다
 */
export function renderLanding(model, opts = {}) {
  const screens = model.screens || [];
  const widgets = screens.flatMap(s => s.widgets || []);
  const body = widgets.map(w => widgetHtml(w, model)).join('\n      ');
  const title = model.links?.[Object.keys(model.links || {})[0]]?.promo || model.meta?.name || '광고';

  // 광고 서버가 붙인 ?imp=… 를 CTA 클릭에 실어 보낸다. 전환을 사슬에 묶기 위한 최소 스크립트.
  const script = opts.impParam === false ? '' : `
<script>
(function(){
  var imp = new URLSearchParams(location.search).get('imp');
  if (!imp) return;
  document.querySelectorAll('[data-cta]').forEach(function(a){
    a.addEventListener('click', function(){
      try { navigator.sendBeacon('/e', JSON.stringify({impId:imp,type:'engage'})); } catch(e){}
    });
  });
})();
</script>`;

  return `<!doctype html>
<html lang="ko">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<meta name="robots" content="noindex">
<title>${esc(title)}</title>
<style>${pageCss(model)}${emitAdStyles({ theme: model.theme }, { unit: 'px' })}</style>
</head>
<body>
  <main class="wrap">
      ${body}
  </main>${script}
</body>
</html>
`;
}

/**
 * 발행 전 검수 — 막을 것과 알릴 것을 나눈다.
 * 광고는 소재가 곧 상품이라, 빈 링크나 누락된 동의 고지가 그대로 나가면 안 된다.
 */
export function reviewLanding(model) {
  const errors = [], warnings = [];
  const widgets = (model.screens || []).flatMap(s => s.widgets || []);
  const types = new Set(widgets.map(w => w.type));

  for (const [k, v] of Object.entries(model.links || {})) {
    if (!v.url || !/^https?:\/\//i.test(v.url)) errors.push(`링크 '${k}' 의 주소가 비었거나 http(s) 가 아니다.`);
  }
  if (!widgets.length) errors.push('위젯이 하나도 없다.');

  // 리드수집·체험단 성격이면 동의 고지가 **필수**다(키즈노트 집행정책과 동형).
  const collectsLead = types.has('adStepGuide') || /lead|trial|survey/i.test(model.meta?.deviceKey || '');
  if (collectsLead && !types.has('adConsent'))
    errors.push('개인정보 수집·활용 고지(adConsent)가 없다 — 리드수집·체험단은 고지 없이 집행할 수 없다.');
  const consent = widgets.find(w => w.type === 'adConsent');
  if (consent && !String(consent.text || '').trim())
    errors.push('개인정보 고지 문구가 비어 있다.');

  if (!types.has('ctaButton') && !types.has('adOfferRow') && !types.has('adProductCard'))
    warnings.push('클릭할 곳(CTA·오퍼·상품카드)이 없다 — 전환이 일어나지 않는다.');
  if (!Object.values(model.links || {}).some(v => v.image))
    warnings.push('소재 이미지가 없다 — 히어로가 비어 보인다.');

  return { ok: errors.length === 0, errors, warnings };
}
