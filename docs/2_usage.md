# Usage Guide

This guide covers how to use the Shoal web interface and configure BMCs and users.

## Web Interface

Access the web interface at `http://localhost:8080`.

- **Dashboard**: Overview of managed BMCs with status and last seen timestamps.
- **BMC Management**: Complete CRUD operations - add, edit, delete, and enable/disable BMCs.
- **Detailed BMC Status**: Click the "Details" button to view comprehensive information about any BMC, including:
  - **System Information**: Serial number, SKU, power state, model, and manufacturer.
  - **Network Interfaces**: NIC details with MAC addresses and IP addresses.
  - **Storage Devices**: Drive information with capacity, model, serial numbers, and health status.
  - **System Event Log (SEL)**: Recent log entries with severity levels and timestamps.
- **Connection Testing**: Quick connectivity tests for any BMC with a one-click "Test" button.
- **Power Control**: Execute power actions (On, ForceOff, ForceRestart) directly from the web UI.
- **Real-time Feedback**: Success/error messaging for all operations.

## BMC Configuration

When adding BMCs, you provide the base URL that represents the BMC's network address. Shoal automatically handles the Redfish API path construction.

### Address Format

The BMC address should be the base URL **without** the `/redfish/v1` suffix.

**Standard BMCs (Physical Hardware):**
- IP address: `192.168.1.100` → Shoal uses `https://192.168.1.100/redfish/v1/...`
- With protocol: `https://192.168.1.100` or `http://192.168.1.100`
- Hostname: `bmc.example.com` → Shoal uses `https://bmc.example.com/redfish/v1/...`
- With port: `192.168.1.100:8443` → Shoal uses `https://192.168.1.100:8443/redfish/v1/...`

**Mock/Testing BMCs:**
- With path prefix: `https://mock.shoal.cloud/public-rackmount1`
  - Shoal preserves the path and appends: `https://mock.shoal.cloud/public-rackmount1/redfish/v1/...`

### Important Notes

- If no protocol is specified, Shoal defaults to HTTPS.
- The `/redfish/v1` path is automatically appended—don't include it in the address.
- For mock servers or proxies with path prefixes, include the full base path.
- Trailing slashes are automatically handled.

## User Management

### Default Administrator

On first run, Shoal creates a default administrator account:
- **Username**: `admin`
- **Password**: `admin`

**IMPORTANT**: Change the default password immediately after first login.

### User Roles

Shoal implements role-based access control with three user roles:

- **Administrator**: Full system access. Can manage users, configure BMCs, and execute all actions.
- **Operator**: BMC management access. Can view and manage BMCs and execute power control actions, but cannot manage users.
- **Viewer**: Read-only access. Can view BMC status and configuration but cannot make any changes.

### User Operations

- **Web Interface** (administrators only): Navigate to "Manage Users" to add, edit, or delete users.
- **User Profile**: All users can access their profile from the menu to change their own password.
