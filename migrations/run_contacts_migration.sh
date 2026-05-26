#!/bin/bash
# Bash script to run contacts table migration using .env configuration
# Usage: ./migrations/run_contacts_migration.sh

set -e

echo "🔍 Loading environment variables from .env files..."

# Load .env.production if it exists, otherwise .env
if [ -f ".env.production" ]; then
    echo "✅ Found .env.production"
    export $(cat .env.production | grep -v '^#' | xargs)
elif [ -f ".env" ]; then
    echo "✅ Found .env"
    export $(cat .env | grep -v '^#' | xargs)
else
    echo "❌ No .env file found!"
    exit 1
fi

# Check if DATABASE_URL is set
if [ -z "$DATABASE_URL" ]; then
    # Build from components
    if [ -n "$DB_HOST" ] && [ -n "$DB_USER" ] && [ -n "$DB_NAME" ]; then
        DB_SSLMODE=${DB_SSLMODE:-disable}
        DATABASE_URL="postgresql://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=${DB_SSLMODE}"
    fi
fi

if [ -z "$DATABASE_URL" ]; then
    echo "❌ DATABASE_URL not configured!"
    echo "Please set DATABASE_URL in .env or provide DB_HOST, DB_USER, DB_NAME, DB_PORT"
    exit 1
fi

echo "🔗 Using database from environment variables"
echo "📝 Running contacts table migration..."

# Check if psql is available
if command -v psql &> /dev/null; then
    psql "$DATABASE_URL" -f migrations/create_contacts_table.sql
    echo "✅ Contacts table migration completed successfully!"
else
    echo "❌ psql command not found!"
    echo "Please install PostgreSQL client tools"
    echo ""
    echo "Alternatively, run this SQL manually:"
    echo "DATABASE_URL: $DATABASE_URL"
    echo "SQL File: migrations/create_contacts_table.sql"
    exit 1
fi
