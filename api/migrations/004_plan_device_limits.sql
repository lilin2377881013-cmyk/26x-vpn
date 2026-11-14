UPDATE plans
SET max_devices = CASE tier WHEN 'standard' THEN 3 WHEN 'premium' THEN 6 END
WHERE (tier = 'standard' AND code IN ('standard-30d', 'standard-90d', 'standard-365d'))
   OR (tier = 'premium' AND code IN ('premium-30d', 'premium-90d', 'premium-365d'));
