-- 为配额与额度相关表增加整数积分列。旧 USD 等值列继续作为兼容与缓存投影，
-- 通过触发器双向同步；所有历史值按固定 1 USD = 100000000 credits 回填。

ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS quota_credits BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS quota_used_credits BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS rate_limit_5h_credits BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS rate_limit_1d_credits BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS rate_limit_7d_credits BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS usage_5h_credits BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS usage_1d_credits BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS usage_7d_credits BIGINT NOT NULL DEFAULT 0;

UPDATE api_keys SET
    quota_credits = ROUND(quota * 100000000)::BIGINT,
    quota_used_credits = ROUND(quota_used * 100000000)::BIGINT,
    rate_limit_5h_credits = ROUND(rate_limit_5h * 100000000)::BIGINT,
    rate_limit_1d_credits = ROUND(rate_limit_1d * 100000000)::BIGINT,
    rate_limit_7d_credits = ROUND(rate_limit_7d * 100000000)::BIGINT,
    usage_5h_credits = ROUND(usage_5h * 100000000)::BIGINT,
    usage_1d_credits = ROUND(usage_1d * 100000000)::BIGINT,
    usage_7d_credits = ROUND(usage_7d * 100000000)::BIGINT;

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS daily_limit_credits BIGINT,
    ADD COLUMN IF NOT EXISTS weekly_limit_credits BIGINT,
    ADD COLUMN IF NOT EXISTS monthly_limit_credits BIGINT;

UPDATE groups SET
    daily_limit_credits = CASE WHEN daily_limit_usd IS NULL THEN NULL ELSE ROUND(daily_limit_usd * 100000000)::BIGINT END,
    weekly_limit_credits = CASE WHEN weekly_limit_usd IS NULL THEN NULL ELSE ROUND(weekly_limit_usd * 100000000)::BIGINT END,
    monthly_limit_credits = CASE WHEN monthly_limit_usd IS NULL THEN NULL ELSE ROUND(monthly_limit_usd * 100000000)::BIGINT END;

ALTER TABLE user_subscriptions
    ADD COLUMN IF NOT EXISTS daily_usage_credits BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS weekly_usage_credits BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS monthly_usage_credits BIGINT NOT NULL DEFAULT 0;

UPDATE user_subscriptions SET
    daily_usage_credits = ROUND(daily_usage_usd * 100000000)::BIGINT,
    weekly_usage_credits = ROUND(weekly_usage_usd * 100000000)::BIGINT,
    monthly_usage_credits = ROUND(monthly_usage_usd * 100000000)::BIGINT;

ALTER TABLE user_platform_quotas
    ADD COLUMN IF NOT EXISTS daily_limit_credits BIGINT,
    ADD COLUMN IF NOT EXISTS weekly_limit_credits BIGINT,
    ADD COLUMN IF NOT EXISTS monthly_limit_credits BIGINT,
    ADD COLUMN IF NOT EXISTS daily_usage_credits BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS weekly_usage_credits BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS monthly_usage_credits BIGINT NOT NULL DEFAULT 0;

UPDATE user_platform_quotas SET
    daily_limit_credits = CASE WHEN daily_limit_usd IS NULL THEN NULL ELSE ROUND(daily_limit_usd * 100000000)::BIGINT END,
    weekly_limit_credits = CASE WHEN weekly_limit_usd IS NULL THEN NULL ELSE ROUND(weekly_limit_usd * 100000000)::BIGINT END,
    monthly_limit_credits = CASE WHEN monthly_limit_usd IS NULL THEN NULL ELSE ROUND(monthly_limit_usd * 100000000)::BIGINT END,
    daily_usage_credits = ROUND(daily_usage_usd * 100000000)::BIGINT,
    weekly_usage_credits = ROUND(weekly_usage_usd * 100000000)::BIGINT,
    monthly_usage_credits = ROUND(monthly_usage_usd * 100000000)::BIGINT;

ALTER TABLE redeem_codes
    ADD COLUMN IF NOT EXISTS value_credits BIGINT NOT NULL DEFAULT 0;

UPDATE redeem_codes
SET value_credits = ROUND(value * 100000000)::BIGINT
WHERE value_credits = 0 AND value <> 0;

CREATE OR REPLACE FUNCTION sync_sub2api_credit_pair(
    is_insert BOOLEAN,
    old_credits BIGINT,
    new_credits BIGINT,
    old_money NUMERIC,
    new_money NUMERIC
)
RETURNS TABLE(credit_value BIGINT, money_value NUMERIC)
LANGUAGE plpgsql
AS $$
DECLARE
    ledger_scale CONSTANT numeric := 100000000;
BEGIN
    IF is_insert THEN
        IF COALESCE(new_credits, 0) = 0 AND COALESCE(new_money, 0) <> 0 THEN
            credit_value := ROUND(new_money * ledger_scale)::BIGINT;
            money_value := new_money;
        ELSIF COALESCE(new_credits, 0) <> 0 THEN
            credit_value := new_credits;
            money_value := new_credits::numeric / ledger_scale;
        ELSE
            credit_value := COALESCE(new_credits, 0);
            money_value := COALESCE(new_money, 0);
        END IF;
        RETURN NEXT;
        RETURN;
    END IF;

    IF new_credits IS DISTINCT FROM old_credits
       AND new_money IS NOT DISTINCT FROM old_money THEN
        credit_value := new_credits;
        money_value := CASE WHEN new_credits IS NULL THEN NULL ELSE new_credits::numeric / ledger_scale END;
    ELSIF new_money IS DISTINCT FROM old_money
          AND new_credits IS NOT DISTINCT FROM old_credits THEN
        credit_value := CASE WHEN new_money IS NULL THEN NULL ELSE ROUND(new_money * ledger_scale)::BIGINT END;
        money_value := new_money;
    ELSE
        credit_value := new_credits;
        money_value := CASE WHEN new_credits IS NULL THEN NULL ELSE new_credits::numeric / ledger_scale END;
    END IF;
    RETURN NEXT;
END;
$$;

CREATE OR REPLACE FUNCTION sync_sub2api_api_key_credit_columns()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    credit_value BIGINT;
    money_value NUMERIC;
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.quota_credits = 0 AND NEW.quota <> 0 THEN NEW.quota_credits := ROUND(NEW.quota * 100000000)::BIGINT; ELSIF NEW.quota_credits <> 0 THEN NEW.quota := NEW.quota_credits::numeric / 100000000; END IF;
        IF NEW.quota_used_credits = 0 AND NEW.quota_used <> 0 THEN NEW.quota_used_credits := ROUND(NEW.quota_used * 100000000)::BIGINT; ELSIF NEW.quota_used_credits <> 0 THEN NEW.quota_used := NEW.quota_used_credits::numeric / 100000000; END IF;
        IF NEW.rate_limit_5h_credits = 0 AND NEW.rate_limit_5h <> 0 THEN NEW.rate_limit_5h_credits := ROUND(NEW.rate_limit_5h * 100000000)::BIGINT; ELSIF NEW.rate_limit_5h_credits <> 0 THEN NEW.rate_limit_5h := NEW.rate_limit_5h_credits::numeric / 100000000; END IF;
        IF NEW.rate_limit_1d_credits = 0 AND NEW.rate_limit_1d <> 0 THEN NEW.rate_limit_1d_credits := ROUND(NEW.rate_limit_1d * 100000000)::BIGINT; ELSIF NEW.rate_limit_1d_credits <> 0 THEN NEW.rate_limit_1d := NEW.rate_limit_1d_credits::numeric / 100000000; END IF;
        IF NEW.rate_limit_7d_credits = 0 AND NEW.rate_limit_7d <> 0 THEN NEW.rate_limit_7d_credits := ROUND(NEW.rate_limit_7d * 100000000)::BIGINT; ELSIF NEW.rate_limit_7d_credits <> 0 THEN NEW.rate_limit_7d := NEW.rate_limit_7d_credits::numeric / 100000000; END IF;
        IF NEW.usage_5h_credits = 0 AND NEW.usage_5h <> 0 THEN NEW.usage_5h_credits := ROUND(NEW.usage_5h * 100000000)::BIGINT; ELSIF NEW.usage_5h_credits <> 0 THEN NEW.usage_5h := NEW.usage_5h_credits::numeric / 100000000; END IF;
        IF NEW.usage_1d_credits = 0 AND NEW.usage_1d <> 0 THEN NEW.usage_1d_credits := ROUND(NEW.usage_1d * 100000000)::BIGINT; ELSIF NEW.usage_1d_credits <> 0 THEN NEW.usage_1d := NEW.usage_1d_credits::numeric / 100000000; END IF;
        IF NEW.usage_7d_credits = 0 AND NEW.usage_7d <> 0 THEN NEW.usage_7d_credits := ROUND(NEW.usage_7d * 100000000)::BIGINT; ELSIF NEW.usage_7d_credits <> 0 THEN NEW.usage_7d := NEW.usage_7d_credits::numeric / 100000000; END IF;
        RETURN NEW;
    END IF;

    SELECT * INTO credit_value, money_value FROM sync_sub2api_credit_pair(false, OLD.quota_credits, NEW.quota_credits, OLD.quota, NEW.quota);
    NEW.quota_credits := credit_value; NEW.quota := money_value;
    SELECT * INTO credit_value, money_value FROM sync_sub2api_credit_pair(false, OLD.quota_used_credits, NEW.quota_used_credits, OLD.quota_used, NEW.quota_used);
    NEW.quota_used_credits := credit_value; NEW.quota_used := money_value;
    SELECT * INTO credit_value, money_value FROM sync_sub2api_credit_pair(false, OLD.rate_limit_5h_credits, NEW.rate_limit_5h_credits, OLD.rate_limit_5h, NEW.rate_limit_5h);
    NEW.rate_limit_5h_credits := credit_value; NEW.rate_limit_5h := money_value;
    SELECT * INTO credit_value, money_value FROM sync_sub2api_credit_pair(false, OLD.rate_limit_1d_credits, NEW.rate_limit_1d_credits, OLD.rate_limit_1d, NEW.rate_limit_1d);
    NEW.rate_limit_1d_credits := credit_value; NEW.rate_limit_1d := money_value;
    SELECT * INTO credit_value, money_value FROM sync_sub2api_credit_pair(false, OLD.rate_limit_7d_credits, NEW.rate_limit_7d_credits, OLD.rate_limit_7d, NEW.rate_limit_7d);
    NEW.rate_limit_7d_credits := credit_value; NEW.rate_limit_7d := money_value;
    SELECT * INTO credit_value, money_value FROM sync_sub2api_credit_pair(false, OLD.usage_5h_credits, NEW.usage_5h_credits, OLD.usage_5h, NEW.usage_5h);
    NEW.usage_5h_credits := credit_value; NEW.usage_5h := money_value;
    SELECT * INTO credit_value, money_value FROM sync_sub2api_credit_pair(false, OLD.usage_1d_credits, NEW.usage_1d_credits, OLD.usage_1d, NEW.usage_1d);
    NEW.usage_1d_credits := credit_value; NEW.usage_1d := money_value;
    SELECT * INTO credit_value, money_value FROM sync_sub2api_credit_pair(false, OLD.usage_7d_credits, NEW.usage_7d_credits, OLD.usage_7d, NEW.usage_7d);
    NEW.usage_7d_credits := credit_value; NEW.usage_7d := money_value;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_sync_sub2api_api_key_credit_columns ON api_keys;
CREATE TRIGGER trg_sync_sub2api_api_key_credit_columns
BEFORE INSERT OR UPDATE OF quota, quota_credits, quota_used, quota_used_credits,
    rate_limit_5h, rate_limit_5h_credits, rate_limit_1d, rate_limit_1d_credits,
    rate_limit_7d, rate_limit_7d_credits, usage_5h, usage_5h_credits,
    usage_1d, usage_1d_credits, usage_7d, usage_7d_credits
ON api_keys
FOR EACH ROW EXECUTE FUNCTION sync_sub2api_api_key_credit_columns();

CREATE OR REPLACE FUNCTION sync_sub2api_group_credit_columns()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    credit_value BIGINT;
    money_value NUMERIC;
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.daily_limit_credits IS NULL AND NEW.daily_limit_usd IS NOT NULL THEN NEW.daily_limit_credits := ROUND(NEW.daily_limit_usd * 100000000)::BIGINT; ELSIF NEW.daily_limit_credits IS NOT NULL THEN NEW.daily_limit_usd := NEW.daily_limit_credits::numeric / 100000000; END IF;
        IF NEW.weekly_limit_credits IS NULL AND NEW.weekly_limit_usd IS NOT NULL THEN NEW.weekly_limit_credits := ROUND(NEW.weekly_limit_usd * 100000000)::BIGINT; ELSIF NEW.weekly_limit_credits IS NOT NULL THEN NEW.weekly_limit_usd := NEW.weekly_limit_credits::numeric / 100000000; END IF;
        IF NEW.monthly_limit_credits IS NULL AND NEW.monthly_limit_usd IS NOT NULL THEN NEW.monthly_limit_credits := ROUND(NEW.monthly_limit_usd * 100000000)::BIGINT; ELSIF NEW.monthly_limit_credits IS NOT NULL THEN NEW.monthly_limit_usd := NEW.monthly_limit_credits::numeric / 100000000; END IF;
        RETURN NEW;
    END IF;
    SELECT * INTO credit_value, money_value FROM sync_sub2api_credit_pair(false, OLD.daily_limit_credits, NEW.daily_limit_credits, OLD.daily_limit_usd, NEW.daily_limit_usd);
    NEW.daily_limit_credits := credit_value; NEW.daily_limit_usd := money_value;
    SELECT * INTO credit_value, money_value FROM sync_sub2api_credit_pair(false, OLD.weekly_limit_credits, NEW.weekly_limit_credits, OLD.weekly_limit_usd, NEW.weekly_limit_usd);
    NEW.weekly_limit_credits := credit_value; NEW.weekly_limit_usd := money_value;
    SELECT * INTO credit_value, money_value FROM sync_sub2api_credit_pair(false, OLD.monthly_limit_credits, NEW.monthly_limit_credits, OLD.monthly_limit_usd, NEW.monthly_limit_usd);
    NEW.monthly_limit_credits := credit_value; NEW.monthly_limit_usd := money_value;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_sync_sub2api_group_credit_columns ON groups;
CREATE TRIGGER trg_sync_sub2api_group_credit_columns
BEFORE INSERT OR UPDATE OF daily_limit_usd, daily_limit_credits,
    weekly_limit_usd, weekly_limit_credits, monthly_limit_usd, monthly_limit_credits
ON groups
FOR EACH ROW EXECUTE FUNCTION sync_sub2api_group_credit_columns();

CREATE OR REPLACE FUNCTION sync_sub2api_subscription_credit_columns()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    credit_value BIGINT;
    money_value NUMERIC;
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.daily_usage_credits = 0 AND NEW.daily_usage_usd <> 0 THEN NEW.daily_usage_credits := ROUND(NEW.daily_usage_usd * 100000000)::BIGINT; ELSIF NEW.daily_usage_credits <> 0 THEN NEW.daily_usage_usd := NEW.daily_usage_credits::numeric / 100000000; END IF;
        IF NEW.weekly_usage_credits = 0 AND NEW.weekly_usage_usd <> 0 THEN NEW.weekly_usage_credits := ROUND(NEW.weekly_usage_usd * 100000000)::BIGINT; ELSIF NEW.weekly_usage_credits <> 0 THEN NEW.weekly_usage_usd := NEW.weekly_usage_credits::numeric / 100000000; END IF;
        IF NEW.monthly_usage_credits = 0 AND NEW.monthly_usage_usd <> 0 THEN NEW.monthly_usage_credits := ROUND(NEW.monthly_usage_usd * 100000000)::BIGINT; ELSIF NEW.monthly_usage_credits <> 0 THEN NEW.monthly_usage_usd := NEW.monthly_usage_credits::numeric / 100000000; END IF;
        RETURN NEW;
    END IF;
    SELECT * INTO credit_value, money_value FROM sync_sub2api_credit_pair(false, OLD.daily_usage_credits, NEW.daily_usage_credits, OLD.daily_usage_usd, NEW.daily_usage_usd);
    NEW.daily_usage_credits := credit_value; NEW.daily_usage_usd := money_value;
    SELECT * INTO credit_value, money_value FROM sync_sub2api_credit_pair(false, OLD.weekly_usage_credits, NEW.weekly_usage_credits, OLD.weekly_usage_usd, NEW.weekly_usage_usd);
    NEW.weekly_usage_credits := credit_value; NEW.weekly_usage_usd := money_value;
    SELECT * INTO credit_value, money_value FROM sync_sub2api_credit_pair(false, OLD.monthly_usage_credits, NEW.monthly_usage_credits, OLD.monthly_usage_usd, NEW.monthly_usage_usd);
    NEW.monthly_usage_credits := credit_value; NEW.monthly_usage_usd := money_value;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_sync_sub2api_subscription_credit_columns ON user_subscriptions;
CREATE TRIGGER trg_sync_sub2api_subscription_credit_columns
BEFORE INSERT OR UPDATE OF daily_usage_usd, daily_usage_credits,
    weekly_usage_usd, weekly_usage_credits, monthly_usage_usd, monthly_usage_credits
ON user_subscriptions
FOR EACH ROW EXECUTE FUNCTION sync_sub2api_subscription_credit_columns();

CREATE OR REPLACE FUNCTION sync_sub2api_platform_quota_credit_columns()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    credit_value BIGINT;
    money_value NUMERIC;
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.daily_limit_credits IS NULL AND NEW.daily_limit_usd IS NOT NULL THEN NEW.daily_limit_credits := ROUND(NEW.daily_limit_usd * 100000000)::BIGINT; ELSIF NEW.daily_limit_credits IS NOT NULL THEN NEW.daily_limit_usd := NEW.daily_limit_credits::numeric / 100000000; END IF;
        IF NEW.weekly_limit_credits IS NULL AND NEW.weekly_limit_usd IS NOT NULL THEN NEW.weekly_limit_credits := ROUND(NEW.weekly_limit_usd * 100000000)::BIGINT; ELSIF NEW.weekly_limit_credits IS NOT NULL THEN NEW.weekly_limit_usd := NEW.weekly_limit_credits::numeric / 100000000; END IF;
        IF NEW.monthly_limit_credits IS NULL AND NEW.monthly_limit_usd IS NOT NULL THEN NEW.monthly_limit_credits := ROUND(NEW.monthly_limit_usd * 100000000)::BIGINT; ELSIF NEW.monthly_limit_credits IS NOT NULL THEN NEW.monthly_limit_usd := NEW.monthly_limit_credits::numeric / 100000000; END IF;
        IF NEW.daily_usage_credits = 0 AND NEW.daily_usage_usd <> 0 THEN NEW.daily_usage_credits := ROUND(NEW.daily_usage_usd * 100000000)::BIGINT; ELSIF NEW.daily_usage_credits <> 0 THEN NEW.daily_usage_usd := NEW.daily_usage_credits::numeric / 100000000; END IF;
        IF NEW.weekly_usage_credits = 0 AND NEW.weekly_usage_usd <> 0 THEN NEW.weekly_usage_credits := ROUND(NEW.weekly_usage_usd * 100000000)::BIGINT; ELSIF NEW.weekly_usage_credits <> 0 THEN NEW.weekly_usage_usd := NEW.weekly_usage_credits::numeric / 100000000; END IF;
        IF NEW.monthly_usage_credits = 0 AND NEW.monthly_usage_usd <> 0 THEN NEW.monthly_usage_credits := ROUND(NEW.monthly_usage_usd * 100000000)::BIGINT; ELSIF NEW.monthly_usage_credits <> 0 THEN NEW.monthly_usage_usd := NEW.monthly_usage_credits::numeric / 100000000; END IF;
        RETURN NEW;
    END IF;
    SELECT * INTO credit_value, money_value FROM sync_sub2api_credit_pair(false, OLD.daily_limit_credits, NEW.daily_limit_credits, OLD.daily_limit_usd, NEW.daily_limit_usd);
    NEW.daily_limit_credits := credit_value; NEW.daily_limit_usd := money_value;
    SELECT * INTO credit_value, money_value FROM sync_sub2api_credit_pair(false, OLD.weekly_limit_credits, NEW.weekly_limit_credits, OLD.weekly_limit_usd, NEW.weekly_limit_usd);
    NEW.weekly_limit_credits := credit_value; NEW.weekly_limit_usd := money_value;
    SELECT * INTO credit_value, money_value FROM sync_sub2api_credit_pair(false, OLD.monthly_limit_credits, NEW.monthly_limit_credits, OLD.monthly_limit_usd, NEW.monthly_limit_usd);
    NEW.monthly_limit_credits := credit_value; NEW.monthly_limit_usd := money_value;
    SELECT * INTO credit_value, money_value FROM sync_sub2api_credit_pair(false, OLD.daily_usage_credits, NEW.daily_usage_credits, OLD.daily_usage_usd, NEW.daily_usage_usd);
    NEW.daily_usage_credits := credit_value; NEW.daily_usage_usd := money_value;
    SELECT * INTO credit_value, money_value FROM sync_sub2api_credit_pair(false, OLD.weekly_usage_credits, NEW.weekly_usage_credits, OLD.weekly_usage_usd, NEW.weekly_usage_usd);
    NEW.weekly_usage_credits := credit_value; NEW.weekly_usage_usd := money_value;
    SELECT * INTO credit_value, money_value FROM sync_sub2api_credit_pair(false, OLD.monthly_usage_credits, NEW.monthly_usage_credits, OLD.monthly_usage_usd, NEW.monthly_usage_usd);
    NEW.monthly_usage_credits := credit_value; NEW.monthly_usage_usd := money_value;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_sync_sub2api_platform_quota_credit_columns ON user_platform_quotas;
CREATE TRIGGER trg_sync_sub2api_platform_quota_credit_columns
BEFORE INSERT OR UPDATE OF daily_limit_usd, daily_limit_credits,
    weekly_limit_usd, weekly_limit_credits, monthly_limit_usd, monthly_limit_credits,
    daily_usage_usd, daily_usage_credits, weekly_usage_usd, weekly_usage_credits,
    monthly_usage_usd, monthly_usage_credits
ON user_platform_quotas
FOR EACH ROW EXECUTE FUNCTION sync_sub2api_platform_quota_credit_columns();

CREATE OR REPLACE FUNCTION sync_sub2api_redeem_code_credit_columns()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.value_credits = 0 AND NEW.value <> 0 THEN NEW.value_credits := ROUND(NEW.value * 100000000)::BIGINT; ELSIF NEW.value_credits <> 0 THEN NEW.value := NEW.value_credits::numeric / 100000000; END IF;
        RETURN NEW;
    END IF;
    IF NEW.value_credits IS DISTINCT FROM OLD.value_credits AND NEW.value IS NOT DISTINCT FROM OLD.value THEN
        NEW.value := NEW.value_credits::numeric / 100000000;
    ELSIF NEW.value IS DISTINCT FROM OLD.value AND NEW.value_credits IS NOT DISTINCT FROM OLD.value_credits THEN
        NEW.value_credits := ROUND(NEW.value * 100000000)::BIGINT;
    ELSE
        NEW.value := NEW.value_credits::numeric / 100000000;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_sync_sub2api_redeem_code_credit_columns ON redeem_codes;
CREATE TRIGGER trg_sync_sub2api_redeem_code_credit_columns
BEFORE INSERT OR UPDATE OF value, value_credits
ON redeem_codes
FOR EACH ROW EXECUTE FUNCTION sync_sub2api_redeem_code_credit_columns();
