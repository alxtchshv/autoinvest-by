package db

// Все суммы — в копейках (INTEGER), количество актива — в 1e-8 долях (INTEGER),
// время — unix-секунды (INTEGER). Так нет ошибок округления float в деньгах.
var migrations = []string{
	`
CREATE TABLE users (
	id               INTEGER PRIMARY KEY,
	email            TEXT NOT NULL UNIQUE COLLATE NOCASE,
	email_verified_at INTEGER,
	phone            TEXT NOT NULL DEFAULT '',
	name             TEXT NOT NULL,
	password_hash    TEXT NOT NULL,
	role             TEXT NOT NULL DEFAULT 'user',      -- user | support | admin
	status           TEXT NOT NULL DEFAULT 'active',    -- active | blocked | deleted
	totp_secret      TEXT NOT NULL DEFAULT '',
	totp_enabled     INTEGER NOT NULL DEFAULT 0,
	recovery_codes   TEXT NOT NULL DEFAULT '',
	tier             TEXT NOT NULL DEFAULT 'free',      -- free | pro
	pro_until        INTEGER,
	pro_autorenew    INTEGER NOT NULL DEFAULT 0,
	pro_period       TEXT NOT NULL DEFAULT 'month',
	referral_code    TEXT NOT NULL UNIQUE,
	referred_by      INTEGER REFERENCES users(id),
	referral_rewarded INTEGER NOT NULL DEFAULT 0,
	risk_profile     TEXT NOT NULL DEFAULT '',
	kyc_status       TEXT NOT NULL DEFAULT 'none',      -- none | pending | approved | rejected
	telegram_chat_id INTEGER,
	telegram_username TEXT NOT NULL DEFAULT '',
	notify_email     INTEGER NOT NULL DEFAULT 1,
	notify_telegram  INTEGER NOT NULL DEFAULT 1,
	notify_market    INTEGER NOT NULL DEFAULT 0,
	created_at       INTEGER NOT NULL,
	updated_at       INTEGER NOT NULL,
	last_login_at    INTEGER
);

CREATE TABLE sessions (
	id           TEXT PRIMARY KEY,                -- sha256(token)
	user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at   INTEGER NOT NULL,
	last_seen_at INTEGER NOT NULL,
	expires_at   INTEGER NOT NULL,
	ip           TEXT NOT NULL DEFAULT '',
	user_agent   TEXT NOT NULL DEFAULT '',
	mfa_pending  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX sessions_user ON sessions(user_id);

CREATE TABLE email_tokens (
	token_hash TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	kind       TEXT NOT NULL,                     -- verify | reset
	expires_at INTEGER NOT NULL,
	used_at    INTEGER
);

CREATE TABLE kyc (
	user_id        INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
	last_name      TEXT NOT NULL,
	first_name     TEXT NOT NULL,
	middle_name    TEXT NOT NULL DEFAULT '',
	birth_date     TEXT NOT NULL,
	personal_number TEXT NOT NULL,
	doc_number     TEXT NOT NULL,
	doc_issued_at  TEXT NOT NULL DEFAULT '',
	citizenship    TEXT NOT NULL DEFAULT 'BY',
	address        TEXT NOT NULL DEFAULT '',
	doc_file       TEXT NOT NULL DEFAULT '',
	selfie_file    TEXT NOT NULL DEFAULT '',
	status         TEXT NOT NULL DEFAULT 'pending',
	reject_reason  TEXT NOT NULL DEFAULT '',
	submitted_at   INTEGER NOT NULL,
	reviewed_at    INTEGER,
	reviewed_by    INTEGER
);

CREATE TABLE cards (
	id         INTEGER PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	provider   TEXT NOT NULL,
	token      TEXT NOT NULL,
	brand      TEXT NOT NULL DEFAULT '',
	last4      TEXT NOT NULL,
	exp_month  INTEGER NOT NULL DEFAULT 0,
	exp_year   INTEGER NOT NULL DEFAULT 0,
	holder     TEXT NOT NULL DEFAULT '',
	issuer     TEXT NOT NULL DEFAULT '',
	is_default INTEGER NOT NULL DEFAULT 0,
	status     TEXT NOT NULL DEFAULT 'active',   -- active | removed
	created_at INTEGER NOT NULL
);
CREATE INDEX cards_user ON cards(user_id, status);

CREATE TABLE card_bindings (
	ref          TEXT PRIMARY KEY,
	user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	provider     TEXT NOT NULL,
	provider_ref TEXT NOT NULL DEFAULT '',
	status       TEXT NOT NULL DEFAULT 'pending', -- pending | done | failed
	card_id      INTEGER,
	error        TEXT NOT NULL DEFAULT '',
	created_at   INTEGER NOT NULL
);

CREATE TABLE plans (
	id             INTEGER PRIMARY KEY,
	user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name           TEXT NOT NULL,
	icon           TEXT NOT NULL DEFAULT 'piggy',
	strategy       TEXT NOT NULL DEFAULT 'custom',
	goal_kop       INTEGER,
	goal_date      TEXT,
	amount_kop     INTEGER NOT NULL,
	frequency      TEXT NOT NULL,                -- daily | weekly | biweekly | monthly | manual
	weekday        INTEGER NOT NULL DEFAULT 1,   -- 1=пн … 7=вс
	monthday       INTEGER NOT NULL DEFAULT 1,
	run_minute     INTEGER NOT NULL DEFAULT 600, -- минута суток по Минску (600 = 10:00)
	card_id        INTEGER REFERENCES cards(id),
	status         TEXT NOT NULL DEFAULT 'active', -- active | paused | closed
	dip_enabled    INTEGER NOT NULL DEFAULT 0,
	dip_threshold_bp INTEGER NOT NULL DEFAULT 500,
	dip_multiplier_pct INTEGER NOT NULL DEFAULT 100,
	last_dip_at    INTEGER,
	next_run_at    INTEGER,
	fail_count     INTEGER NOT NULL DEFAULT 0,
	created_at     INTEGER NOT NULL,
	updated_at     INTEGER NOT NULL,
	closed_at      INTEGER
);
CREATE INDEX plans_user ON plans(user_id, status);
CREATE INDEX plans_due ON plans(status, next_run_at);

CREATE TABLE plan_allocations (
	plan_id   INTEGER NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
	asset     TEXT NOT NULL,
	weight_bp INTEGER NOT NULL,
	PRIMARY KEY (plan_id, asset)
);

CREATE TABLE positions (
	plan_id  INTEGER NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
	asset    TEXT NOT NULL,
	qty      INTEGER NOT NULL DEFAULT 0,   -- 1e-8
	cost_kop INTEGER NOT NULL DEFAULT 0,   -- себестоимость
	PRIMARY KEY (plan_id, asset)
);

CREATE TABLE payments (
	id           INTEGER PRIMARY KEY,
	user_id      INTEGER NOT NULL REFERENCES users(id),
	card_id      INTEGER REFERENCES cards(id),
	provider     TEXT NOT NULL,
	kind         TEXT NOT NULL,                -- charge | payout | subscription
	tracking_id  TEXT NOT NULL UNIQUE,
	provider_uid TEXT NOT NULL DEFAULT '',
	amount_kop   INTEGER NOT NULL,
	status       TEXT NOT NULL,                -- pending | successful | failed
	message      TEXT NOT NULL DEFAULT '',
	test         INTEGER NOT NULL DEFAULT 1,
	created_at   INTEGER NOT NULL,
	updated_at   INTEGER NOT NULL
);

CREATE TABLE orders (
	id          INTEGER PRIMARY KEY,
	user_id     INTEGER NOT NULL REFERENCES users(id),
	plan_id     INTEGER NOT NULL REFERENCES plans(id),
	side        TEXT NOT NULL,                 -- buy | sell
	trigger     TEXT NOT NULL,                 -- schedule | manual | dip | close
	status      TEXT NOT NULL,                 -- pending | filled | failed | payout_pending
	amount_kop  INTEGER NOT NULL,              -- списано с карты / выплачено на карту
	fee_kop     INTEGER NOT NULL DEFAULT 0,
	idem_key    TEXT NOT NULL UNIQUE,
	payment_id  INTEGER REFERENCES payments(id),
	usd_byn     REAL NOT NULL DEFAULT 0,
	error       TEXT NOT NULL DEFAULT '',
	broker      TEXT NOT NULL DEFAULT 'paper',
	created_at  INTEGER NOT NULL,
	filled_at   INTEGER
);
CREATE INDEX orders_user ON orders(user_id, created_at);
CREATE INDEX orders_plan ON orders(plan_id, created_at);

CREATE TABLE fills (
	id        INTEGER PRIMARY KEY,
	order_id  INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
	asset     TEXT NOT NULL,
	qty       INTEGER NOT NULL,
	price_byn REAL NOT NULL,
	price_usd REAL NOT NULL,
	amount_kop INTEGER NOT NULL
);
CREATE INDEX fills_order ON fills(order_id);

CREATE TABLE snapshots (
	user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	plan_id     INTEGER NOT NULL DEFAULT 0,      -- 0 = весь портфель
	ts          INTEGER NOT NULL,
	value_kop   INTEGER NOT NULL,
	invested_kop INTEGER NOT NULL,
	PRIMARY KEY (user_id, plan_id, ts)
);

CREATE TABLE notifications (
	id         INTEGER PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	kind       TEXT NOT NULL,
	title      TEXT NOT NULL,
	body       TEXT NOT NULL DEFAULT '',
	link       TEXT NOT NULL DEFAULT '',
	read_at    INTEGER,
	created_at INTEGER NOT NULL
);
CREATE INDEX notifications_user ON notifications(user_id, created_at);

CREATE TABLE tickets (
	id         INTEGER PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	subject    TEXT NOT NULL,
	category   TEXT NOT NULL DEFAULT 'other',
	status     TEXT NOT NULL DEFAULT 'open',     -- open | answered | closed
	assignee_id INTEGER,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE INDEX tickets_user ON tickets(user_id);
CREATE INDEX tickets_status ON tickets(status, updated_at);

CREATE TABLE ticket_messages (
	id         INTEGER PRIMARY KEY,
	ticket_id  INTEGER NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
	author_id  INTEGER NOT NULL REFERENCES users(id),
	is_staff   INTEGER NOT NULL DEFAULT 0,
	body       TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX ticket_messages_ticket ON ticket_messages(ticket_id);

CREATE TABLE telegram_links (
	code       TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at INTEGER NOT NULL
);

CREATE TABLE audit_log (
	id         INTEGER PRIMARY KEY,
	user_id    INTEGER,
	actor_id   INTEGER,
	action     TEXT NOT NULL,
	details    TEXT NOT NULL DEFAULT '',
	ip         TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE INDEX audit_user ON audit_log(user_id, created_at);

CREATE TABLE kv (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`,
	// 2: один паспорт — один аккаунт; защита от повторного использования TOTP-кода
	`
CREATE UNIQUE INDEX kyc_personal_number ON kyc(personal_number);
ALTER TABLE users ADD COLUMN totp_last_step INTEGER NOT NULL DEFAULT 0;
`,
	// 3: User-Agent в журнале действий, согласия с версиями документов (Закон № 99-З требует
	// уметь доказать факт согласия), первые 6 цифр карты для маски, отметка о прохождении онбординга
	`
ALTER TABLE audit_log ADD COLUMN user_agent TEXT NOT NULL DEFAULT '';
CREATE TABLE consents (
	id          INTEGER PRIMARY KEY,
	user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	kind        TEXT NOT NULL,                  -- offer | risks | privacy
	version     TEXT NOT NULL,
	accepted_at INTEGER NOT NULL,
	ip          TEXT NOT NULL DEFAULT '',
	user_agent  TEXT NOT NULL DEFAULT '',
	revoked_at  INTEGER
);
CREATE INDEX consents_user ON consents(user_id, kind);
ALTER TABLE cards ADD COLUMN bin TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN onboarded_at INTEGER;
`,
}
