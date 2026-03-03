# Calendar App - Development Guide

This document provides context and guidelines for working on the calendar application.

## Overview

The calendar app is a standard calendar application that allows users to view their own calendar and others' calendars. It supports the standard features users expect from calendar apps, including:

- Creating and managing calendar events
- RSVP functionality (going, interested, not going)
- Viewing events from multiple calendars
- Event invitations and notifications
- Event details including locations, times, descriptions, and status

The app is built as an **experimental application** to explore the constraints and capabilities of private data on ATProto. It has been instrumental in figuring out the notification/inbox system required to notify users of new invites and updates to events when other people are invited.

## Architecture

### Frontend Stack

The frontend uses:

- **Vite** - Build tool and dev server
- **TanStack Query** (React Query) - Server state management and data fetching
- **TanStack Router** - Routing
- **React** - UI framework
- **React Hook Form** - Form handling

### Backend Architecture

The backend is **atypical** for a calendar app. It uses **Pear** (see `cmd/pear/main.go`), which is an experimental version of ATProto with permissioned data.

Key architectural points:

1. **All access through the user's Habitat node**: The Habitat node's API hides the complexity of working with the different Habitat Nodes/PDSs of all the involved users. All queries for data should go to the logged in user's Habitat node.

2. **No appview/appserver**: Because this is an experimental app, there is no ATProto appview or appserver. The frontend talks directly to the Habitat node, which acts as a stand-in for a PDS (Personal Data Server).

### Data Model

The app uses two main lexicon types:

#### Events (`community.lexicon.calendar.event`)

The base data for an event. Will be stored on the event creator's node.

#### RSVPs (`community.lexicon.calendar.rsvp`)

The rsvp to an event. Will be stored on the responder's node. There can be many rsvps for an event, split across many nodes. The Habitat API handles merging this data together.

See the lexicon definitions:

- `lexicons/community/lexicon/calendar/event.json`
- `lexicons/community/lexicon/calendar/rsvp.json`

## Development Workflow

### Moon Commands

This project uses [Moon](https://moonrepo.dev) as the build system. Common commands:

#### Type Checking

```bash
moon run calendar:build
```

This runs `tsc --build` to type-check the TypeScript code.

#### Running Tests

```bash
moon run calendar:test
```

This runs `vitest run` to execute the test suite.

#### Development Server

```bash
moon run calendar:dev
```

This starts the development environment, which includes:

1. **Vite dev server** - Serves the frontend application
2. **Tailscale Funnel** - Uses `cmd/funnel/main.go` to expose the local server via Tailscale

**Important**: The dev server setup means you can only run **one instance at a time**. The funnel is configured to proxy port 5173 (Vite's default port) to a Tailscale hostname based on the project name.

The dev task also has dependencies:

- `pear:dev` - Starts the Habitat/Pear backend server
- `funnel:start` - Starts the Tailscale funnel proxy

#### Other Useful Commands

```bash
# Format code
moon run calendar:format

# Check formatting
moon run calendar:format-check

# Lint code
moon run calendar:lint-check

# Fix linting issues
moon run calendar:lint
```

### Project Structure

The core controller for this project is:

- **`src/controllers/eventController.ts`** - Contains all the business logic for events and RSVPs

This controller provides:

- `createEvent()` - Creates a new event and automatically sends notifications to invited DIDs
- `listEvents()` - Lists all events the user has access to
- `listRsvps()` - Lists all RSVPs with their corresponding event info

The controller follows a "fetch the world" pattern for RSVPs - it fetches all events and RSVPs upfront, then matches them in memory. This is necessary because of the distributed nature of the data. This also simplifies the implementation of querying for data.

## Code Style & Philosophy

### TanStack Query Philosophy

Follow the [TanStack Query philosophy](https://tanstack.com/query/latest/docs/framework/react/overview) as much as possible:

- **Server state is different**: Server state is persisted remotely, requires async APIs, can be changed by others, and can become "out of date"
- **Let TanStack Query handle the hard parts**: Caching, deduping requests, background updates, refetching, pagination, etc.
- **Use query keys properly**: Structure query keys to enable proper invalidation and caching
- **Leverage built-in features**: Use features like `staleTime`, `cacheTime`, `refetchOnWindowFocus`, etc. appropriately
- **Optimistic updates**: Use mutations with optimistic updates for better UX when appropriate

Key principles:

- Don't manually manage loading states, error states, or caching - let TanStack Query handle it
- Use query invalidation to keep data fresh
- Structure queries to match the data model and access patterns

### Code Organization

1. **Separate UX from Controllers**:
   - Keep business logic in controllers (like `eventController.ts`)
   - Keep UI components focused on presentation and user interaction
   - Controllers should be pure functions that work with the `HabitatClient`
   - Components should go in the `components` folder. Components should have no idea how Habitat works and just interact with controllers, which express their API in terms of the application at hand

2. **Maintainable, Readable Code**:
   - Write clear, self-documenting code
   - Use TypeScript types effectively
   - Add comments for complex logic or non-obvious decisions
   - Follow consistent naming conventions

3. **Break Down Tasks**:
   - When working on features, break them into subtasks
   - Be proactive about generating a plan before jumping into coding
   - Consider the data flow: how does data move from the backend through controllers to components?

#### Controller Pattern

Controllers should:

- Accept `HabitatClient` as a parameter (don't create clients inside controllers)
- Return typed data structures
- Handle data transformation (like matching RSVPs to events)
- Be testable in isolation
- Use dependency injection / functional principles as much as possible

## Styling

The calendar app uses the same CSS approach as the `frontend` app:

### CSS Framework: Pico CSS

- **Pico CSS** is loaded from CDN in `index.html` - a minimal CSS framework that provides semantic HTML styling
- No build-time CSS processing needed
- Uses semantic HTML elements (`<article>`, `<button>`, `<form>`, `<label>`, etc.) that are automatically styled by Pico

### Global Styles

- **`src/style.css`** - Global stylesheet for custom CSS variables and overrides
- Imported in `index.html` after Pico CSS
- Use CSS custom properties (CSS variables) for theming, following the same pattern as the frontend app

### Styling Approach

1. **Use Pico CSS classes and semantic HTML**: Leverage Pico's built-in styling for forms, buttons, articles, etc.
2. **CSS Custom Properties**: Define theme colors and values using CSS variables in `style.css`
3. **Inline styles for dynamic values**: Use React's `style` prop for dynamic, component-specific styling
4. **className for static classes**: Use `className` for static CSS classes when needed

### Example

```css
/* src/style.css */
body {
  --pico-primary-background: #bcd979;
  --pico-primary-border: #9dad6f;
  --pico-primary-inverse: #2d2925;
  --pico-primary-hover-background: #9dad6f;
  --pico-primary-hover-border: #9dad6f;
}

/* Custom calendar-specific styles */
.calendar-grid {
  display: grid;
  grid-template-columns: repeat(7, 1fr);
}
```

```tsx
// In components
<article>
  <h1>Calendar</h1>
  <form>
    <label>
      Event Name:
      <input type="text" />
    </label>
    <button type="submit">Create</button>
  </form>
</article>
```

## Working with the Backend

### HabitatClient

The `HabitatClient` (from `internal/habitatClient.ts`) is the interface to the Habitat node. Key methods:

- `putPrivateRecord<T>()` - Create or update a private record with optional grantees
- `getPrivateRecord<T>()` - Get a private record (checks permissions and notifications)
- `listPrivateRecords<T>()` - List all private records in a collection
- Do not use `listNotifications`, this will be deprecated
