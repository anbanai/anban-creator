-- Account-specific billing multiplier for dynamic model/media operation costs.
-- NULL means use the neutral account multiplier 1.0000; tier multipliers are
-- applied separately. Values such as 1.0500 allow low-margin operation billing
-- without changing user tier.

ALTER TABLE users
  ADD COLUMN billing_multiplier DECIMAL(8,4) NULL;
