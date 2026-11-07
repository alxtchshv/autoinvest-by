export type Asset = {
  symbol: string; name: string; kind: "stable" | "gold" | "crypto" | "stock" | "bond"; risk: number; color: string;
  short: string; description: string; listed_since: string; pro_only: boolean; indicative: boolean;
  coupon_bp?: number; face_currency?: string;
};
export type Strategy = { id: string; name: string; description: string; risk: number; weights: Record<string, number> };
export type Quote = {
  symbol: string; price_usd: number; price_byn: number; change_24h: number; high_24h: number; low_24h: number;
  volume_usd: number; source: string; updated_at: string; stale: boolean;
};
export type Rates = { usd: number; eur: number; rub100: number; date: string; updated_at: string; source: string; stale: boolean };
export type Candle = { t: number; o: number; h: number; l: number; c: number; v: number };
export type Stats = { return_1y: number; return_30d: number; volatility: number; max_drawdown: number; high_1y: number; low_1y: number; days: number; worst_day: number; best_day: number };

export type Config = {
  mode: "sandbox" | "bepaid_test" | "live"; payments: string; broker: string; market_demo: boolean; telegram_bot: string;
  fee_free_bp: number; fee_pro_bp: number; spread_bp: number; pro_month_kop: number; pro_year_kop: number;
  min_contrib_kop: number; max_contrib_kop: number; free_plan_limit: number; pro_plan_limit: number; require_kyc: boolean;
  assets: Asset[]; strategies: Strategy[]; version: string; legal_version: string;
  telegram_test?: boolean; auth?: { google: "sandbox" | "on" | "off"; apple: "sandbox" | "on" | "off"; phone: string };
};

export type User = {
  id: number; email: string; email_verified: boolean; phone: string; phone_verified?: boolean; has_password?: boolean; name: string; role: "user" | "support" | "admin";
  status: string; totp_enabled: boolean; tier: string; pro_until: number | null; pro_autorenew: boolean; pro_period: string;
  referral_code: string; risk_profile: string; kyc_status: "none" | "pending" | "approved" | "rejected"; telegram_username: string;
  notify_email: boolean; notify_telegram: boolean; notify_market: boolean; created_at: number; last_login_at: number | null; onboarded_at: number | null;
};
export type Me = { user: User | null; is_pro?: boolean; unread?: number; cards?: number; plans?: number; telegram_linked?: boolean; mfa_required?: boolean; mfa?: { totp: boolean; passkey: boolean }; passkeys?: number };

export type Card = { id: number; provider: string; brand: string; last4: string; bin: string; mask: string; exp_month: number; exp_year: number; holder: string; issuer: string; is_default: boolean; status: string; created_at: number };
export type AuditEvent = { action: string; details: string; ip: string; user_agent: string; created_at: number };
export type Consent = { kind: "offer" | "risks" | "privacy"; version: string; accepted_at: number; ip: string; revoked_at: number | null };
export type Alloc = { asset: string; weight_bp: number };
export type PositionView = { asset: string; qty: number; cost_kop: number; value_kop: number; price_byn: number; change_24h: number; share_bp: number; stale: boolean };
export type Valuation = { value_kop: number; invested_kop: number; pnl_kop: number; pnl_pct: number; day_pnl_kop: number; positions: PositionView[]; stale: boolean };

export type Plan = {
  id: number; name: string; icon: string; strategy: string; goal_kop: number | null; goal_date: string | null; amount_kop: number;
  frequency: "daily" | "weekly" | "biweekly" | "monthly" | "manual"; weekday: number; monthday: number; run_minute: number;
  card_id: number | null; status: "active" | "paused" | "closed"; dip_enabled: boolean; dip_threshold_bp: number; dip_multiplier_pct: number;
  next_run_at: number | null; fail_count: number; created_at: number; closed_at: number | null; allocations: Alloc[];
  valuation: Valuation; card: Card | null; risk: number; monthly_kop: number; goal_progress_bp: number; goal_eta: string | null;
  goal_need_kop: number | null; next_runs: number[];
};

export type Fill = { asset: string; qty: number; price_byn: number; price_usd: number; amount_kop: number };
export type Order = {
  id: number; plan_id: number; plan_name: string; side: "buy" | "sell"; trigger: string;
  status: "pending" | "filled" | "failed" | "payout_pending" | "manual"; amount_kop: number; fee_kop: number; usd_byn: number;
  error: string; broker: string; created_at: number; filled_at: number | null; card_last4: string; card_brand: string;
  payment_uid: string; provider: string; test: boolean; fills: Fill[]; user_email?: string; user_id?: number;
};
export type Snapshot = { t: number; value_kop: number; invested_kop: number };
export type Notification = { id: number; kind: string; title: string; body: string; link: string; read: boolean; created_at: number };
export type TicketMessage = { id: number; author_name: string; is_staff: boolean; body: string; created_at: number };
export type Ticket = {
  id: number; user_id: number; user_email?: string; user_name?: string; subject: string; category: string;
  status: "open" | "answered" | "closed"; created_at: number; updated_at: number; last_message: string; messages?: TicketMessage[];
};
export type KYC = {
  user_id: number; user_email?: string; last_name: string; first_name: string; middle_name: string; birth_date: string;
  personal_number: string; doc_number: string; doc_issued_at: string; citizenship: string; address: string;
  has_doc: boolean; has_selfie: boolean; status: string; reject_reason: string; submitted_at: number; reviewed_at: number | null;
  method: "manual" | "msi" | "msi-sandbox" | "docs" | "docs-sandbox";
};
export type KYCMethods = { msi: "sandbox" | "oauth" | "off"; docs: "sandbox" | "sumsub" | "manual" };
export type Session = { id: string; created_at: number; last_seen_at: number; ip: string; user_agent: string; current: boolean };
export type Payment = { id: number; kind: string; amount_kop: number; status: string; message: string; test: boolean; created_at: number; card_last4: string; provider_uid: string };

export type BacktestResult = {
  points: { t: number; invested_kop: number; value_kop: number; usd_kop: number; deposit_kop: number }[];
  invested_kop: number; value_kop: number; usd_kop: number; deposit_kop: number; profit_pct: number; max_drawdown: number;
  purchases: number; from: string; to: string; note: string; rates_history: boolean;
};
