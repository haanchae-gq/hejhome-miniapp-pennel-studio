#!/usr/bin/env node
/**
 * export.mjs — 생성 저장소를 **git 인수인계**로 내보낸다 (P3).
 *
 *   node src/export.mjs <repoDir> [--remote <url>] [--push] [--message "<msg>"]
 *
 * 저작도구가 만든 Ray 저장소(out/<id>/)를 개발자에게 넘길 수 있는 git 저장소로 만든다.
 * .gitignore 를 쓰고, git init 하고, 인수인계 요약을 커밋 메시지에 담아 한 커밋으로 묶는다.
 * HANDOFF.md·HANDOFF.html 이 이미 저장소 안에 있어, git 으로 전달되면 그 자체가 인수인계다.
 *
 * push 는 부작용이라 **--push 를 줄 때만** 한다. 원격은 --remote 로 사용자가 지정한다.
 * 커밋 아이덴티티는 저장소 config 를 건드리지 않도록 `-c` 로만 준다(공용 호스트 배려).
 */
import { execFileSync } from 'node:child_process';
import { existsSync, writeFileSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const GITIGNORE = `# miniapp-panel-studio export
node_modules/
dist/
.DS_Store
*.log
yarn-error.log
`;

const AUTHOR = ['-c', 'user.name=miniapp-panel-studio', '-c', 'user.email=devteam@goqual.com'];

function git(cwd, args, { allowFail = false } = {}) {
  try {
    return execFileSync('git', args, { cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
  } catch (e) {
    if (allowFail) return null;
    throw new Error(`git ${args.join(' ')} 실패: ${e.stderr || e.message}`);
  }
}

/** HANDOFF.md 에서 인수인계 요약을 뽑아 커밋 메시지에 쓴다. */
function handoffSummary(repoDir) {
  const p = resolve(repoDir, 'HANDOFF.md');
  if (!existsSync(p)) return { line: '', blockers: 0 };
  const md = readFileSync(p, 'utf8');
  const rest = md.match(/남은 항목:\s*\*\*(.+?)\*\*/);
  const blk = md.match(/blocker\s*(\d+)/i);
  return { line: rest ? rest[1] : '', blockers: blk ? Number(blk[1]) : 0 };
}

export function exportRepo(repoDir, { remote, push = false, message } = {}) {
  const dir = resolve(repoDir);
  if (!existsSync(dir)) throw new Error(`저장소 경로 없음: ${dir}`);
  // 완전 저장소(package.json) 또는 blocker 로 멈춘 인수인계-only 디렉터리(HANDOFF.md) 둘 다 허용.
  if (!existsSync(resolve(dir, 'package.json')) && !existsSync(resolve(dir, 'HANDOFF.md')))
    throw new Error(`생성 저장소가 아닌 것 같다(package.json·HANDOFF.md 없음): ${dir}`);

  // .gitignore
  const giPath = resolve(dir, '.gitignore');
  if (!existsSync(giPath)) writeFileSync(giPath, GITIGNORE);

  // git init (없으면)
  const fresh = !existsSync(resolve(dir, '.git'));
  if (fresh) {
    git(dir, ['init', '-q']);
    git(dir, ['checkout', '-q', '-b', 'main'], { allowFail: true }); // 기본 브랜치 main
  }

  const { line, blockers } = handoffSummary(dir);
  const name = (() => { try { return JSON.parse(readFileSync(resolve(dir, 'package.json'), 'utf8')).name; } catch { return 'panel'; } })();
  const msg = message ||
    `chore: ${name} 인수인계 (miniapp-panel-studio 생성)\n\n` +
    `남은 항목: ${line || '?'}\n` +
    `개발자는 HANDOFF.md / HANDOFF.html 을 먼저 보라.\n` +
    (blockers ? `\n⚠ 빌드 blocker ${blockers}건 — 이걸 채우기 전엔 ray build 가 되지 않는다.\n` : '');

  git(dir, ['add', '-A']);
  const staged = git(dir, ['status', '--porcelain'], { allowFail: true });
  let committed = false;
  if (staged) {
    git(dir, [...AUTHOR, 'commit', '-q', '-m', msg]);
    committed = true;
  }

  if (remote) {
    const has = git(dir, ['remote'], { allowFail: true }) || '';
    if (has.split(/\s+/).includes('origin')) git(dir, ['remote', 'set-url', 'origin', remote]);
    else git(dir, ['remote', 'add', 'origin', remote]);
  }

  let pushed = false;
  if (push) {
    if (!remote && !(git(dir, ['remote'], { allowFail: true }) || '').includes('origin'))
      throw new Error('--push 하려면 --remote <url> 이 필요하다(origin 미설정).');
    git(dir, ['push', '-u', 'origin', 'main']);
    pushed = true;
  }

  const head = git(dir, ['rev-parse', '--short', 'HEAD'], { allowFail: true });
  return { dir, fresh, committed, head, blockers, remote: remote || null, pushed };
}

// CLI
if (process.argv[1] && resolve(process.argv[1]) === (await import('node:url')).fileURLToPath(import.meta.url)) {
  const argv = process.argv.slice(2);
  const repoDir = argv.find(a => !a.startsWith('--'));
  const remote = (i => (i >= 0 ? argv[i + 1] : undefined))(argv.indexOf('--remote'));
  const message = (i => (i >= 0 ? argv[i + 1] : undefined))(argv.indexOf('--message'));
  const push = argv.includes('--push');
  if (!repoDir) {
    console.error('usage: node src/export.mjs <repoDir> [--remote <url>] [--push] [--message "<msg>"]');
    process.exit(1);
  }
  try {
    const r = exportRepo(repoDir, { remote, push, message });
    console.log(`\n✔ git 인수인계 export → ${r.dir}`);
    console.log(`  ${r.fresh ? 'git init(main)' : '기존 git'} · ${r.committed ? `커밋 ${r.head}` : '변경 없음(커밋 생략)'}`);
    if (r.blockers) console.log(`  ⚠ 빌드 blocker ${r.blockers}건 — HANDOFF 를 보고 채워야 ray build 된다`);
    if (r.remote) console.log(`  origin: ${r.remote}${r.pushed ? ' (푸시됨)' : ' (미푸시 — --push 로 올린다)'}`);
    if (!r.remote) {
      console.log(`\n  다음: 원격을 붙여 올린다`);
      console.log(`    git -C ${r.dir} remote add origin <url>`);
      console.log(`    git -C ${r.dir} push -u origin main`);
    }
    console.log('');
  } catch (e) {
    console.error(`\n✘ ${e.message}\n`);
    process.exit(1);
  }
}
