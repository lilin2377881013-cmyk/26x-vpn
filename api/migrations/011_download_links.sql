CREATE TABLE download_links (
    channel TEXT PRIMARY KEY CHECK (channel IN ('ios-tf','ios-store','macos','windows','android')),
    url TEXT NOT NULL
);
