-- Eligibility follows the current subscription, including after a paid upgrade.
CREATE VIEW active_trial_users AS
SELECT DISTINCT s.user_id FROM subscriptions s JOIN plans p ON p.id=s.plan_id
WHERE p.code='trial-15m' AND s.status='active' AND s.starts_at<=now() AND s.ends_at>now()
AND NOT EXISTS (
    SELECT 1 FROM subscriptions paid WHERE paid.user_id=s.user_id AND paid.plan_id<>s.plan_id
    AND paid.status='active' AND paid.starts_at<=now() AND (paid.ends_at IS NULL OR paid.ends_at>now())
);
