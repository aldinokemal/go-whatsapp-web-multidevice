# Project Analysis & Documentation

## 1. Project Structure

The project follows a **Clean Architecture** (or Hexagonal Architecture) approach, tailored for a Go application. This separation of concerns makes the application testable and maintainable.

### Key Directories:

*   **`cmd/`**: Entry point of the application.
*   **`config/`**: Configuration management (environment variables, flags).
*   **`domains/`**: Domain definitions (Entities, Interfaces). This is the "core" of the application, defining *what* the application does without worrying about *how*.
    *   `app/`, `chat/`, `chatstorage/`, `group/`, `message/`, `user/`: Define interfaces for UseCases and Repositories, and data structures (structs).
*   **`usecase/`**: Business logic implementation.
    *   `app.go`, `chat.go`, `message.go`: Implements the interfaces defined in `domains/`. This layer coordinates data flow between the UI (controllers) and Infrastructure (database/WhatsApp).
*   **`infrastructure/`**: External tools and implementations.
    *   `whatsapp/`: Implementation of the WhatsApp client using `whatsmeow`. Handles connection, event listening, and media downloading.
    *   `chatstorage/`: Database implementation (SQLite/Postgres) for storing chats and messages.
*   **`ui/`**: User Interface and Entry Points.
    *   `http/`: REST API handlers (using Fiber).
    *   `websocket/`: WebSocket server implementation.
*   **`views/`**: Frontend code (HTML, CSS, JS).
    *   `index.html`: Main entry point.
    *   `components/`: Vue.js components for different features.
*   **`pkg/`**: Shared utilities (errors, helper functions).

## 2. Technologies

### Backend
*   **Language:** Go (Golang) 1.24+
*   **Web Framework:** [Fiber](https://gofiber.io/) (Fast, Express-inspired web framework)
*   **WhatsApp Library:** [whatsmeow](https://go.mau.fi/whatsmeow) (Go implementation of WhatsApp Web API)
*   **Database:** SQLite (default) or PostgreSQL. Used to store session data (`whatsapp.db`) and chat history (`chatstorage.db`).
*   **WebSockets:** `github.com/gofiber/websocket/v2` for real-time communication (currently limited use).
*   **Logging:** `logrus` for structured logging.

### Frontend
*   **Framework:** Vue.js 3 (using ES modules via CDN/direct import, no build step like Webpack/Vite visible in the analyzed files).
*   **UI Framework:** Fomantic-UI (Semantic UI fork) for styling.
*   **HTTP Client:** Axios.
*   **State Management:** Reactive `data()` in Vue components; no global store (Pinia/Vuex) observed, although `index.html` manages some global socket state.

## 3. Workflow

1.  **Initialization:**
    *   The app starts, connects to the database, and initializes the WhatsApp client (`whatsmeow`).
    *   It sets up HTTP routes (REST API) and a WebSocket endpoint (`/ws`).
2.  **Authentication:**
    *   User opens the frontend.
    *   Frontend connects via WebSocket.
    *   User requests a QR code (via API). Backend gets QR from `whatsmeow` and sends it to Frontend (or Frontend polls for it).
    *   User scans QR. `whatsmeow` receives `PairSuccess` event.
    *   Session data is saved to `whatsapp.db`.
3.  **Message Handling (Incoming):**
    *   `whatsmeow` receives a message event from WhatsApp servers.
    *   The `handler` in `infrastructure/whatsapp/init.go` processes it.
    *   Data is saved to `chatstorage.db` (SQLite).
    *   **Missing Step:** The event is *logged* and *forwarded to webhook* (if configured), but **NOT** broadcasted to the frontend via WebSocket.
4.  **Data Retrieval (Frontend):**
    *   User interacts with the UI (e.g., clicks "View Messages").
    *   Vue component (`ChatMessages.js`) calls the REST API (`/chat/:jid/messages`).
    *   Backend queries `chatstorage.db` and returns JSON.
    *   Frontend renders the list.

## 4. Performance

*   **Go & Fiber:** High performance and low memory footprint compared to Node.js/Python alternatives.
*   **SQLite:** Fast for single-file local deployments. Good for desktop/personal use.
*   **whatsmeow:** Efficient reverse-engineered implementation, much lighter than running a headless browser (Puppeteer/Selenium).
*   **Frontend:** Vue.js is lightweight. However, the current "polling" or "refresh-to-update" model is inefficient for user experience, though it saves server resources by avoiding active socket management for high-frequency chat updates.

## 5. Features (Pros & Cons)

### Features (Pros)
*   **Multi-Device Support:** Uses the modern MD protocol.
*   **No Headless Browser:** Low resource usage (CPU/RAM).
*   **Clean Architecture:** Code is organized and easy to extend.
*   **Self-Hosted:** Full control over data; no third-party API fees.
*   **Comprehensive API:** Supports sending text, media, location, contacts, and managing groups.
*   **Webhook Support:** Can push events to external systems.

### Limitations (Cons) & The "Real-Time" Issue
*   **No Real-Time UI Updates:** The biggest limitation. The backend receives messages instantly, but the frontend doesn't know about them until the user manually refreshes or triggers an action.
*   **Limited WebSocket Usage:** WebSockets are currently used mainly for the Login/Logout flow (QR code status, device list), not for chat activity.
*   **Frontend Architecture:** The frontend is "loosely" coupled. It's a collection of Vue components injected into a generic HTML page, making complex state management (like a global live chat store) harder than a dedicated SPA build.
