/**
 * db.mjs — 패널 스토어 (S3). 유저(owner)별로 격리한다.
 *
 * DATABASE_URL 이 있으면 **Postgres**, 없으면 **파일 스토어**로 폴백한다.
 * 이렇게 두면 로컬 개발(파일)과 운영(Postgres, 스택에 이미 있음)이 같은 코드로 돈다.
 * owner 는 Authelia 의 Remote-User(S4)에서 오며, 없으면 'anonymous' 버킷.
 *
 * 인터페이스(모두 async): init · list(owner) · get(id,owner) · create(owner,name,model)
 *                        · save(id,owner,name,model) · del(id,owner)
 */
import { readFileSync, writeFileSync, existsSync, mkdirSync, readdirSync, unlinkSync } from 'node:fs';
import { resolve, join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { randomUUID } from 'node:crypto';

const safe = s => String(s || 'anonymous').replace(/[^\w.@-]/g, '_') || 'anonymous';
const now = () => new Date().toISOString();

/* ── 파일 스토어 (폴백) ──────────────────────────────────────────────── */
function fileStore() {
  const DATA = resolve(dirname(fileURLToPath(import.meta.url)), 'data/panels');
  const ownerDir = o => join(DATA, safe(o));
  const pf = (o, id) => join(ownerDir(o), id.replace(/[^\w-]/g, '') + '.json');
  return {
    kind: 'file',
    async init() { mkdirSync(DATA, { recursive: true }); },
    async list(owner) {
      const d = ownerDir(owner); if (!existsSync(d)) return [];
      return readdirSync(d).filter(f => f.endsWith('.json')).map(f => { const j = JSON.parse(readFileSync(join(d, f), 'utf8')); return { id: j.id, name: j.name, updatedAt: j.updatedAt }; })
        .sort((a, b) => (b.updatedAt || '').localeCompare(a.updatedAt || ''));
    },
    async get(id, owner) { const f = pf(owner, id); return existsSync(f) ? JSON.parse(readFileSync(f, 'utf8')) : null; },
    async create(owner, name, model) {
      mkdirSync(ownerDir(owner), { recursive: true });
      const id = randomUUID().slice(0, 8);
      writeFileSync(pf(owner, id), JSON.stringify({ id, owner: safe(owner), name: name || '새 패널', model, updatedAt: now() }));
      return { id };
    },
    async save(id, owner, name, model) {
      mkdirSync(ownerDir(owner), { recursive: true });
      const updatedAt = now();
      writeFileSync(pf(owner, id), JSON.stringify({ id, owner: safe(owner), name: name || '이름 없음', model, updatedAt }));
      return { id, updatedAt };
    },
    async del(id, owner) { const f = pf(owner, id); if (existsSync(f)) unlinkSync(f); return { ok: true }; },
  };
}

/* ── Postgres 스토어 ─────────────────────────────────────────────────── */
async function pgStore(url) {
  const { default: pg } = await import('pg');
  const pool = new pg.Pool({ connectionString: url, max: 5 });
  return {
    kind: 'postgres', pool,
    async init() {
      await pool.query(`CREATE TABLE IF NOT EXISTS studio_panels (
        id text PRIMARY KEY, owner text NOT NULL, name text,
        model jsonb NOT NULL, updated_at timestamptz NOT NULL DEFAULT now())`);
      await pool.query(`CREATE INDEX IF NOT EXISTS studio_panels_owner ON studio_panels(owner)`);
    },
    async list(owner) {
      const { rows } = await pool.query('SELECT id, name, updated_at FROM studio_panels WHERE owner=$1 ORDER BY updated_at DESC', [safe(owner)]);
      return rows.map(r => ({ id: r.id, name: r.name, updatedAt: r.updated_at.toISOString() }));
    },
    async get(id, owner) {
      const { rows } = await pool.query('SELECT id, name, model, updated_at FROM studio_panels WHERE id=$1 AND owner=$2', [id, safe(owner)]);
      if (!rows[0]) return null;
      return { id: rows[0].id, name: rows[0].name, model: rows[0].model, updatedAt: rows[0].updated_at.toISOString() };
    },
    async create(owner, name, model) {
      const id = randomUUID().slice(0, 8);
      await pool.query('INSERT INTO studio_panels(id, owner, name, model) VALUES($1,$2,$3,$4)', [id, safe(owner), name || '새 패널', model]);
      return { id };
    },
    async save(id, owner, name, model) {
      const { rows } = await pool.query(
        `INSERT INTO studio_panels(id, owner, name, model, updated_at) VALUES($1,$2,$3,$4,now())
         ON CONFLICT (id) DO UPDATE SET name=$3, model=$4, updated_at=now() WHERE studio_panels.owner=$2
         RETURNING updated_at`, [id, safe(owner), name || '이름 없음', model]);
      return { id, updatedAt: (rows[0]?.updated_at || new Date()).toISOString() };
    },
    async del(id, owner) { await pool.query('DELETE FROM studio_panels WHERE id=$1 AND owner=$2', [id, safe(owner)]); return { ok: true }; },
  };
}

let _store;
export async function store() {
  if (_store) return _store;
  _store = process.env.DATABASE_URL ? await pgStore(process.env.DATABASE_URL) : fileStore();
  await _store.init();
  console.log(`  패널 스토어: ${_store.kind}${_store.kind === 'postgres' ? '' : ' (DATABASE_URL 설정 시 Postgres)'}`);
  return _store;
}
