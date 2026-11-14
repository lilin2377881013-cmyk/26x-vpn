ALTER TABLE devices
    ADD COLUMN ios_system TEXT,
    ADD COLUMN ios_version TEXT,
    ADD CONSTRAINT devices_ios_system_check CHECK (
        (ios_system IS NULL AND ios_version IS NULL)
        OR (platform = 'ios' AND ios_system IS NOT NULL AND ios_version IS NOT NULL
            AND ios_system IN ('iOS', 'iPadOS')
            AND ios_version ~ '^[0-9]{1,3}([.][0-9]{1,3}){0,2}$')
    );
