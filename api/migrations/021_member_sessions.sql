-- Older releases did not distinguish member portal and app sessions.
-- Preserve sessions that are usable at rollout, then bind each on its next use.
ALTER TABLE sessions ADD COLUMN member_channel TEXT CHECK (member_channel IN ('app','portal','legacy'));
ALTER TABLE sessions ALTER COLUMN expires_at DROP NOT NULL;
ALTER TABLE sessions ADD CONSTRAINT sessions_persistent_member CHECK
    (expires_at IS NOT NULL OR (actor_kind='user' AND member_channel IS NOT NULL));

UPDATE sessions s SET expires_at=NULL, member_channel='legacy'
FROM users u WHERE s.actor_kind='user' AND s.actor_id=u.id AND u.status='active'
    AND s.revoked_at IS NULL AND s.expires_at > clock_timestamp()
    AND NOT EXISTS (SELECT 1 FROM trial_accounts t WHERE t.user_id=u.id);
