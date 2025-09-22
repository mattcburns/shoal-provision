# Milestone 011: Configuration Profiles UI (Snapshot)

## 1. Overview

This milestone builds upon the read-only UI established in Milestone 010. It introduces the first write operation: the ability to "snapshot" the current configuration of a BMC and save it as a new version of a Configuration Profile.

This feature will be accessible from the BMC Details page, providing a direct and intuitive workflow for capturing a baseline configuration.

## 2. Rationale

- **User Workflow**: The most common first step in using profiles is to capture a "golden" or baseline configuration from a known-good machine. This feature directly enables that workflow.
- **Scoped Interaction**: This change is limited to a single button and a modal form, making it a well-defined and manageable task for an agent.
- **API Utilization**: It leverages the existing `POST /api/profiles/snapshot` API endpoint, focusing the work on the UI/frontend implementation.

## 3. Scope

### 3.1. Key Features

- **Snapshot Button**: On the BMC Details page (`/bmcs/details?name={bmc-name}`), specifically on the "Settings" tab, add a "Snapshot Configuration" button.
- **Snapshot Modal**:
  - Clicking the button will open a modal dialog.
  - The modal will contain a form with two options:
    1.  **Create New Profile**: A text input for the `name` of a new profile. A `description` field is optional.
    2.  **Add to Existing Profile**: A dropdown/select menu populated with existing profiles.
  - A "Save Snapshot" button within the modal will trigger the snapshot action.
- **Client-Side Logic**:
  - JavaScript is required to handle opening the modal and submitting the form data to the Shoal API.
  - The submission will be an AJAX `POST` request to the `POST /api/profiles/snapshot?bmc={bmc-name}` endpoint.
  - The request body will contain either `{ "name": "new-profile-name", "description": "..." }` or `{ "profile_id": "existing-profile-id" }`.
  - Upon a successful API response, the modal should close, and a success notification (toast/alert) should be displayed.
  - On failure, an error notification should be displayed with a relevant message.

### 3.2. Out of Scope

- A dedicated "Create Profile" page. Profile creation is handled implicitly through the snapshot modal.
- Editing or deleting profiles.
- Applying or diffing profiles.

## 4. Technical Design

### 4.1. Web Layer (`internal/web`)

- **Handler Modifications**:
  - `bmcDetailPage(w, r)`: When rendering the BMC details, this handler must now also fetch a list of all existing configuration profiles (`database.GetConfigProfiles()`). This list will be passed to the template to populate the "Add to Existing Profile" dropdown in the modal.
- **Template Modifications**:
  - `templates/bmc_detail.html` (or the template for the "Settings" tab):
    - Add the HTML for the "Snapshot Configuration" button.
    - Add the HTML structure for the modal dialog, including the form, inputs, and select menu. The select menu should be populated by iterating over the profiles passed from the handler.
- **Static Assets (`static/js`)**:
  - A new or existing JavaScript file will need to contain the logic for:
    - An event listener for the "Snapshot Configuration" button to open the modal.
    - An event listener for the modal's form submission.
    - The `fetch` call to the snapshot API endpoint, constructing the correct URL and body based on form input.
    - Handling the success and error responses from the API.

### 4.2. API Layer (`internal/api`)

- No changes are expected. The `POST /api/profiles/snapshot` endpoint should already support creating a new profile or adding a version to an existing one based on the request body. The agent should verify this behavior and implement it if it's missing.

### 4.3. Database Layer (`internal/database`)

- No changes are expected, as the required `GetConfigProfiles()` function should already exist from the previous milestone.

## 5. Acceptance Criteria

- An AI agent can implement these changes in a single pull request.
- The "Snapshot Configuration" button is visible on the BMC Details/Settings page.
- Clicking the button opens a modal with a form.
- The dropdown in the modal is correctly populated with the names of existing profiles.
- Submitting the form with a new profile name successfully creates a new profile and a new version containing the snapshot.
- Submitting the form with an existing profile selected successfully creates a new version for that profile.
- The user receives clear success or error feedback after the operation.
- All existing tests, including the `build.py validate` pipeline, must pass.
- New JavaScript behavior should be tested manually or with new end-to-end tests if the framework is available.
