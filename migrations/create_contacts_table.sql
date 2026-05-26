-- Create contacts table for contact form submissions
-- This migration can be run independently using:
-- psql $DATABASE_URL < migrations/create_contacts_table.sql

CREATE TABLE IF NOT EXISTS contacts (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) NOT NULL,
    subject VARCHAR(500) NOT NULL,
    message TEXT NOT NULL,
    ip_address VARCHAR(45),
    user_agent TEXT,
    status VARCHAR(50) DEFAULT 'new',
    admin_note TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

-- Create index on deleted_at for soft deletes
CREATE INDEX IF NOT EXISTS idx_contacts_deleted_at ON contacts(deleted_at);

-- Create index on status for filtering
CREATE INDEX IF NOT EXISTS idx_contacts_status ON contacts(status);

-- Create index on created_at for sorting
CREATE INDEX IF NOT EXISTS idx_contacts_created_at ON contacts(created_at DESC);

-- Grant permissions (adjust based on your setup)
-- GRANT ALL PRIVILEGES ON contacts TO your_db_user;
