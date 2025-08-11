# Aggrovites - Copilot Instructions

## Overview
Aggrovites is a simple Go-based invitation and RSVP web application with a unique dual personality system. The app serves different UI themes based on the hostname or query parameters - either an "aggressive/aggro" theme or a "nice/polite" theme.

## Technology Stack
- **Language**: Go 1.21+
- **Web Framework**: Gin (github.com/gin-gonic/gin)
- **Database**: GORM ORM with SQLite (default) or SQL Server support
- **Templates**: HTML templates using Gin's built-in template system
- **Deployment**: Docker with optional Fly.io configuration

## Application Architecture

### Core Files
- `main.go`: Main application logic, HTTP server setup, routing, and handlers
- `types.go`: Data models (Event, RSVP) and business logic methods
- `templates/`: HTML templates for UI rendering
  - `create.tmpl`: Event creation form
  - `event.tmpl`: Event display and RSVP form
- `assets/`: Static assets (CSS, images)

### Database Models
- **Event**: Core event entity with description, start time, timezone, and associated RSVPs
- **RSVP**: Response entity linked to events, includes attendee name, guest count, and declined status

### Key Features
1. **Event Creation**: Simple form to create events with date/time and timezone support
2. **RSVP System**: Attendees can accept/decline with guest count
3. **Dual Personality UI**: Different themes based on hostname detection
4. **Calendar Integration**: Google Calendar and Outlook export links
5. **Database Flexibility**: Supports both SQLite and SQL Server

## Important Patterns & Conventions

### Dual Personality System
The `isNice()` function determines UI personality based on:
- Hostname starting with "nice" (e.g., nice.example.com)
- URL query parameter `host=nice`

**Aggressive Theme** (default):
- Casual, edgy language ("fuck yeah", "Bitch you coming?")
- Domain: aggrovites.northbriton.net
- Branding: "Aggrovite"

**Nice Theme**:
- Polite, formal language ("My pleasure", "Be delighted to have you")
- Domain: nicevites.northbriton.net  
- Branding: "Nicevite"

### Database Configuration
Environment variables determine database connection:
- `MSSQL_DSN`: SQL Server connection string (takes priority)
- `SQLLITE_FILE`: SQLite file path (defaults to "test.db")

### Template System
- Uses Gin's `LoadHTMLGlob("templates/*")` for template loading
- Templates receive `gin.H` objects with different content based on personality
- Shared template structure with dynamic content injection

### Error Handling
- Consistent `errorPage(err, c)` function for error responses
- Validation methods on models (e.g., `Event.Validate()`)
- HTTP status code handling and redirects

## Development Guidelines

### Code Style
- Standard Go formatting and conventions
- GORM patterns for database operations
- Gin middleware and handler patterns
- Template-driven UI with server-side rendering

### Database Operations
- Use GORM AutoMigrate for schema updates
- Preload relationships when needed: `Preload("Rsvps")`
- Validation before database operations
- Consistent error handling for database operations

### Testing & Building
- **Build**: Use `go build .` or `./build.sh` for Docker
- **Run locally**: `go run .` (uses SQLite by default)
- **Dependencies**: Managed via `go mod` (run `go mod tidy` after changes)

### Template Development
- Templates use Go's template syntax with Gin helpers
- Dynamic content through `gin.H` objects
- Personality-specific content injection
- Static assets served from `/assets` route

## Common Development Tasks

### Adding New Routes
1. Define handler function in `main.go`
2. Add route to router with appropriate HTTP method
3. Follow existing patterns for parameter binding and validation

### Database Changes
1. Update models in `types.go`
2. Add validation methods if needed
3. Test with both SQLite and SQL Server if possible
4. GORM handles migrations automatically via AutoMigrate

### UI Changes
1. Modify templates in `templates/` directory
2. Update both personality themes in handler logic
3. Test with both nice and aggressive themes
4. Update static assets in `assets/` if needed

### Adding Business Logic
1. Add methods to appropriate model structs in `types.go`
2. Follow existing patterns (e.g., `Winners()`, `Losers()`, `Total()`)
3. Maintain immutability where possible
4. Add validation as needed

## Environment Setup
- Go 1.21+ required
- SQLite works out of the box
- For SQL Server: set `MSSQL_DSN` environment variable
- Static files served from `./assets`
- Templates loaded from `./templates`

## Deployment Notes
- Dockerized application with multi-stage build
- Fly.io configuration available in `fly.toml`
- Build script creates versioned Docker images
- Application runs on port 9000 by default
- Health check endpoint available at `/ready`