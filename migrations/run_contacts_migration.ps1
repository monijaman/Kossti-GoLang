# PowerShell script to run contacts table migration using .env configuration
# Usage: .\migrations\run_contacts_migration.ps1

Write-Host "🔍 Loading environment variables from .env files..." -ForegroundColor Cyan

# Load .env.production if it exists, otherwise .env
if (Test-Path ".env.production") {
    Write-Host "✅ Found .env.production" -ForegroundColor Green
    Get-Content ".env.production" | ForEach-Object {
        if ($_ -match '^([^#][^=]+)=(.+)$') {
            [System.Environment]::SetEnvironmentVariable($matches[1], $matches[2])
        }
    }
} elseif (Test-Path ".env") {
    Write-Host "✅ Found .env" -ForegroundColor Green
    Get-Content ".env" | ForEach-Object {
        if ($_ -match '^([^#][^=]+)=(.+)$') {
            [System.Environment]::SetEnvironmentVariable($matches[1], $matches[2])
        }
    }
} else {
    Write-Host "❌ No .env file found!" -ForegroundColor Red
    exit 1
}

# Get DATABASE_URL from environment
$DATABASE_URL = [System.Environment]::GetEnvironmentVariable("DATABASE_URL")

if (-not $DATABASE_URL) {
    # Build from components
    $DB_HOST = [System.Environment]::GetEnvironmentVariable("DB_HOST")
    $DB_PORT = [System.Environment]::GetEnvironmentVariable("DB_PORT") 
    $DB_USER = [System.Environment]::GetEnvironmentVariable("DB_USER")
    $DB_PASSWORD = [System.Environment]::GetEnvironmentVariable("DB_PASSWORD")
    $DB_NAME = [System.Environment]::GetEnvironmentVariable("DB_NAME")
    $DB_SSLMODE = [System.Environment]::GetEnvironmentVariable("DB_SSLMODE")
    
    if (-not $DB_SSLMODE) { $DB_SSLMODE = "disable" }
    
    if ($DB_HOST -and $DB_USER -and $DB_NAME) {
        $DATABASE_URL = "postgresql://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=${DB_SSLMODE}"
    }
}

if (-not $DATABASE_URL) {
    Write-Host "❌ DATABASE_URL not configured!" -ForegroundColor Red
    Write-Host "Please set DATABASE_URL in .env or provide DB_HOST, DB_USER, DB_NAME, DB_PORT" -ForegroundColor Yellow
    exit 1
}

Write-Host "🔗 Using database from environment variables" -ForegroundColor Cyan
Write-Host "📝 Running contacts table migration..." -ForegroundColor Cyan

# Check if psql is available
if (Get-Command psql -ErrorAction SilentlyContinue) {
    & psql $DATABASE_URL -f "migrations\create_contacts_table.sql"
    if ($LASTEXITCODE -eq 0) {
        Write-Host "✅ Contacts table migration completed successfully!" -ForegroundColor Green
    } else {
        Write-Host "❌ Migration failed with exit code $LASTEXITCODE" -ForegroundColor Red
        exit $LASTEXITCODE
    }
} else {
    Write-Host "❌ psql command not found!" -ForegroundColor Red
    Write-Host "Please install PostgreSQL client tools" -ForegroundColor Yellow
    Write-Host ""
    Write-Host "Alternatively, run this SQL manually:" -ForegroundColor Yellow
    Write-Host "DATABASE_URL: $DATABASE_URL" -ForegroundColor White
    Write-Host "SQL File: migrations\create_contacts_table.sql" -ForegroundColor White
    exit 1
}
