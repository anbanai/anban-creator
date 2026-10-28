CREATE TABLE IF NOT EXISTS task_shares (
    task_id CHAR(36) NOT NULL PRIMARY KEY,
    user_id CHAR(36) NOT NULL,
    token VARCHAR(64) COLLATE utf8mb4_bin NOT NULL,
    expires_at DATETIME(3) NOT NULL,
    revoked_at DATETIME(3) NULL,
    created_at DATETIME(3) NULL,
    updated_at DATETIME(3) NULL,
    UNIQUE KEY idx_task_share_token (token),
    KEY idx_task_shares_user_id (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
