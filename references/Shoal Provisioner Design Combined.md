# **Design Document: Layer 3 Bare Metal Provisioning System**

Version: 4.0  
Date: November 3, 2025

### **1\. Executive Summary**

This document outlines a high-speed, Layer 3-only provisioning system for bare metal servers. It is designed to replace legacy PXE/L2-dependent methods (like Ironic) with a simpler, more robust, and self-contained architecture.

The architecture is based on a **dual virtual media** model, controlled entirely by **Redfish**. A **static, universal bootc-based maintenance OS** (maintenance.iso) is mounted as the first virtual CD. A **dynamic, job-specific task.iso** is generated on-demand by a controller and mounted as the second virtual CD.

This task.iso contains a recipe.json file, which is read by a Go binary on the maintenance OS. The Go binary acts as a simple dispatcher, starting a systemd target defined in the recipe. All provisioning logic is handled by systemd and **Quadlet**, which execute all tools (e.g., sgdisk, tar, wimapply, firmware updaters) in isolated, privileged containers.

This model is self-contained (no runtime network "phone-home"), supports custom partitioning, and can deploy traditional, mutable **Linux** and **Windows** operating systems, as well as immutable **VMWare ESXi**.

### **2\. Core Principles & Design Goals**

This architecture is built on the following key decisions:

* **Redfish-Only Control Plane:** The system is pure Layer 3 (IP-based). All server control (power, boot, media) is executed via out-of-band Redfish API calls. **PXE, DHCP, and TFTP are not used.**  
* **Dual Virtual Media Boot:** Provisioning is initiated by booting from two virtual ISOs. This avoids the need for a runtime metadata service (like the "phone-home" model) and the slowness of an "on-demand image build" model.  
* **Self-Contained Logic:** The maintenance OS is 100% self-contained after boot. All instructions are read from the locally-mounted task.iso.  
* **Immutable Maintenance, Mutable Production:** The **maintenance.iso** is an immutable bootc-based OS, making it reliable and reproducible. The **target OS** (Linux/Windows) is a traditional, mutable installation.  
* **Isolated Tooling (Containerized):** All provisioning tools are run in privileged containers. This keeps the maintenance OS minimal and clean, and elegantly solves dependency conflicts (e.g., using a Debian-based container to run a vendor's .deb tool).  
* **Declarative Orchestration:** **systemd** is the in-OS orchestrator. Provisioning jobs are defined as systemd targets, providing robust dependency management, logging, and error handling. **Quadlet** (.container files) provides a simple, declarative way to run containers as systemd services.  
* **Lightweight Dispatcher:** A single, static **Go binary** (/usr/sbin/provisioner) is the *only* custom logic that runs on boot. It is fast, dependency-free, and its only job is to parse the recipe and start the appropriate systemd target.  
* **Centralized Artifact Store:** An **OCI Registry** is the single source of truth for all versioned artifacts, including the bootc base, all tool containers, and all target OS filesystem images (.tar.gz, .wim).

### **3\. System Architecture**

The system consists of four primary components:

#### **1\. The Controller (Your "Mini-Ironic" Service)**

* **Role:** The user-facing API and the Redfish orchestrator.  
* **API:** Receives a request from a user (e.g., POST /api/v1/provision). The request body *is* the recipe.  
* **Actions:**  
  1. Receives the recipe.json payload from the user.  
  2. Validates the recipe against the master recipe.schema.json.  
  3. Dynamically generates a small task.iso containing the recipe.json and any associated config files (e.g., user-data, unattend.xml).  
  4. Selects the correct "Installer" ISO based on the recipe (e.g., bootc-maintenance.iso or VMware-VMvisor-Installer.iso).  
  5. Uses Redfish to mount both ISOs to the target server.  
  6. Uses Redfish to set SetOneTimeBoot and reboot the server.  
  7. Waits for a "success" or "failure" webhook from the server.  
  8. Uses Redfish to unmount media and reboot the server into its final OS.

#### **2\. The bootc-maintenance.iso (Virtual CD 1\)**

* **Role:** The universal "workhorse" for all Linux/Windows provisioning and maintenance tasks.  
* **Build:** Built from a bootc base (e.g., Fedora) using a multi-stage Containerfile to compile and copy the static Go binary.  
* **Key Contents:**  
  * /usr/sbin/provisioner: The Go binary (dispatcher).  
  * /etc/systemd/system/provision-dispatcher.service: The service that runs the Go binary on boot.  
  * podman, oras: Core tools.  
  * A pre-built library of systemd units (.target, .service) and Quadlet (.container) files.  
  * All tool containers (e.g., tools/sgdisk, tools/wimapply) are **logically bound** to the bootc image, ensuring they are pre-pulled and available offline.

#### **3\. The task.iso (Virtual CD 2\)**

* **Role:** The dynamic, job-specific "recipe" file.  
* **Build:** Generated on-demand by the controller in seconds.  
* **Key Contents:**  
  * /recipe.json: The main instructions (task target, URLs, partition layout).  
  * /recipe.schema.json: Used by the provisioner to validate the recipe.  
  * /user-data: (Optional) The cloud-init file.  
  * /unattend.xml: (Optional) The Windows answer file.  
  * /ks.cfg: (Optional) The VMWare answer file.

#### **4\. The OCI Registry**

* **Role:** The central artifact store.  
* **Key Contents:**  
  * /bootc-maintenance-base:latest  
  * /tools/sgdisk:1.0 (contains sgdisk, mkfs)  
  * /tools/wimapply:1.0 (contains wimlib-tools, ntfs-3g)  
  * /tools/linux-bootloader:1.0 (contains grub2-efi, efibootmgr)  
  * /tools/supermicro-flasher:1.2 (contains vendor tool \+ wrapper script)  
  * /os-images/ubuntu-rootfs:22.04 (a .tar.gz artifact)  
  * /os-images/windows-wim:2022 (a .wim artifact)

### **5\. Detailed Provisioning Workflows**

#### **Workflow A: Provisioning Traditional Linux**

1. **Controller:** Receives an API request. It generates a task.iso containing recipe.json (with task\_target: "install-linux.target") and a user-data file.  
2. **Redfish:** Controller mounts bootc-maintenance.iso (CD1) and task.iso (CD2), then reboots.  
3. Go Dispatcher (on boot):  
   a. Mounts /dev/sr1 (the task.iso).  
   b. Validates /mnt/task/recipe.json against /mnt/task/recipe.schema.json.  
   c. Writes all recipe variables (e.g., OCI\_URL) to /run/provision/recipe.env and files (e.g., user-data, layout.json) to /run/provision/.  
   d. Runs systemctl start install-linux.target.  
4. systemd (on maintenance OS):  
   a. install-linux.target starts.  
   b. It Requires (and thus runs in order): partition.service, image-linux.service, bootloader-linux.service, and config-drive.service.  
5. Quadlet (on maintenance OS):  
   a. partition.service starts sgdisk-tool.container. This container reads /run/provision/layout.json and creates the partitions on /dev/sda.  
   b. image-linux.service starts oras-imager.container. This container reads OCI\_URL from the env file, pulls the rootfs.tar.gz, and unpacks it to /mnt/new-root.  
   c. bootloader-linux.service starts linux-bootloader.container. This container runs the chroot logic to install GRUB onto /mnt/new-root.  
   d. config-drive.service starts config-drive-tool.container. This container reads /run/provision/user-data and creates the cidata partition.  
6. **Cleanup:** The install-linux.target's OnSuccess hook runs, firing the provision-success.service which sends a webhook to the controller. The controller unmounts media and reboots.

#### **Workflow B: Provisioning Windows Server**

1. **Controller:** Generates a task.iso containing recipe.json (with task\_target: "install-windows.target") and an unattend.xml file.  
2. **Redfish:** Mounts bootc-maintenance.iso (CD1) and task.iso (CD2), then reboots.  
3. **Go Dispatcher:** Boots, mounts task.iso, validates, writes configs to /run/provision, and runs systemctl start install-windows.target.  
4. **systemd:** install-windows.target starts. It Requires (in order): partition.service, image-windows.service, and bootloader-windows.service.  
5. Quadlet:  
   a. partition.service starts sgdisk-tool.container (which also has mkfs.ntfs). It creates the EFI, MSR, and main NTFS partitions.  
   b. image-windows.service starts wim-imager.container. This container reads OCI\_URL, pulls the .wim artifact, and runs wimapply to unpack it to /mnt/new-windows.  
   c. bootloader-windows.service starts windows-bootloader.container. This container copies the boot files from /mnt/new-windows/Windows/Boot/EFI to the EFI partition.  
   d. A final service copies the /run/provision/unattend.xml to /mnt/new-windows/Windows/Panther/.  
6. **Cleanup:** The OnSuccess hook fires, the controller unmounts media and reboots.

#### **Workflow C: Provisioning VMWare ESXi**

1. **Controller:** Receives a request. It generates a task.iso containing only a ks.cfg file.  
2. **Redfish:** Controller mounts **VMware-VMvisor-Installer.iso** (CD1) and the new task.iso (CD2), then reboots.  
3. ESXi Installer (on boot):  
   a. The server boots from the official VMWare installer (CD1).  
   b. The installer automatically finds ks.cfg on the second CD (CD2).  
   c. It performs the fully unattended Kickstart installation.  
   d. It reboots as defined in the Kickstart file.  
4. **Cleanup:** The controller must poll the server's power state. Once it detects the reboot, it unmounts the media.

#### **Workflow D: Ad-Hoc Maintenance (e.g., Firmware Update)**

1. **Controller:** Receives an API request. It generates a task.iso containing recipe.json (with task\_target: "supermicro-update.target" and firmware\_url: "http://...").  
2. **Redfish:** Mounts bootc-maintenance.iso (CD1) and task.iso (CD2), then reboots.  
3. **Go Dispatcher:** Boots, validates, writes FIRMWARE\_URL to /run/provision/recipe.env, and runs systemctl start supermicro-update.target.  
4. **systemd:** supermicro-update.target starts, which simply Requires=supermicro-tool.service.  
5. Quadlet: supermicro-tool.service starts supermicro-flasher.container. This container:  
   a. Reads FIRMWARE\_URL from the environment.  
   b. curls the firmware file to /tmp.  
   c. Runs the vendor's flash tool (e.g., socflash \-f /tmp/firmware.rom).  
6. **Cleanup:** The OnSuccess hook fires, the controller unmounts media and reboots.

### **6\. Error Handling & Feedback Loop**

The provisioning process is asynchronous. The controller knows the job status via webhooks.

* **Pre-flight Errors:** The Go provisioner binary is wrapped in try/catch logic. If it fails (e.g., cannot mount /dev/sr1, recipe.json is invalid), it will call a failure webhook.  
* **Runtime Errors:** The master systemd targets (e.g., install-linux.target) are defined with OnSuccess= and OnFailure=.  
  * **/etc/systemd/system/install-linux.target (partial):**  
    Ini, TOML  
    \[Unit\]  
    ...  
    OnSuccess\=provision-success.service  
    OnFailure\=provision-failed.service

  * provision-success.service sends a webhook to the controller: curl \-X POST .../job-status/${SERIAL\_NUMBER} \-d '{"status": "success"}'.  
  * provision-failed.service sends a webhook: curl \-X POST .../job-status/${SERIAL\_NUMBER} \-d '{"status": "failed", "failed\_step": "${SYSTEMD\_FAILED\_UNIT}"}'. This tells the controller *exactly which service* failed (e.g., bootloader-linux.service).

### **7\. Schema Definition (recipe.schema.json)**

The recipe.json is the "API" for all provisioning tasks. A central recipe.schema.json (included in the task.iso) is used to validate it.

JSON

{  
  "$schema": "http://json-schema.org/draft-07/schema\#",  
  "$id": "http://my-provisioner.com/recipe.schema.json",  
  "title": "Provisioning Recipe",  
  "description": "Defines a provisioning or maintenance task.",  
  "type": "object",  
  "required": \[ "task\_target" \],  
  "properties": {  
    "task\_target": {  
      "description": "The master systemd target to start (e.g., 'install-linux.target', 'supermicro-update.target').",  
      "type": "string"  
    },  
    "target\_disk": {  
      "description": "The block device to install to (e.g., '/dev/sda').",  
      "type": "string"  
    },  
    "oci\_url": {  
      "description": "The OCI registry URL for the OS artifact (e.g., 'my-registry.com/os-images:ubuntu-22.04').",  
      "type": "string"  
    },  
    "firmware\_url": {  
      "description": "A URL to a firmware file for a maintenance task.",  
      "type": "string"  
    },  
    "partition\_layout": {  
      "description": "The custom partition layout for the target disk.",  
      "type": "array",  
      "items": {  
        "type": "object",  
        "required": \["size", "type\_guid"\],  
        "properties": {  
          "size": { "type": "string", "description": "Size in MB ('512M') or percent ('100%')." },  
          "type\_guid": { "type": "string", "description": "GUID or alias for sgdisk (e.g., 'ef00', '8300')." },  
          "format": { "type": "string", "description": "Filesystem (e.g., 'vfat', 'ext4', 'ntfs')." }  
        }  
      }  
    }  
  }  
}

---

## **Design Addendum 4.1: API Endpoints**

### **1\. User-Facing API**

This is the API your users (or other automation) will call to initiate a task.

---

#### **🌎 POST /api/v1/jobs**

This is the main endpoint used to provision or manage a server. It creates a new "job" resource.

* **Action:** Kicks off a new provisioning task (e.g., "install Ubuntu on server X").  
* **Request Body:** The request body *is* the recipe. It must specify the target server (e.g., by serial number, which your controller can map to a BMC IP) and the full, validated recipe.json payload.

**Example Request (curl):**

Bash

curl \-X POST 'http://controller.api/api/v1/jobs' \\  
\-d '{  
    "server\_serial": "XF-12345ABC",  
    "recipe": {  
        "task\_target": "install-linux.target",  
        "target\_disk": "/dev/sda",  
        "oci\_url": "my-registry.com/os-images/ubuntu-rootfs:22.04",  
        "user\_data": "IyEvYmluL2Jhc2gKaG9zdG5hbWUgLXAgc2VydmVyMDEK...",  
        "partition\_layout": \[  
            { "size": "512M", "type\_guid": "ef00", "format": "vfat" },  
            { "size": "100%", "type\_guid": "8300", "format": "ext4" }  
        \]  
    }  
}'

* **Controller's Actions (Synchronous):**  
  1. Receives the request.  
  2. Generates a unique job\_id (e.g., a UUID).  
  3. Validates the recipe section against its master recipe.schema.json. (If invalid, returns 400 Bad Request).  
  4. Stores the job and its recipe in a database, marked as "queued".  
  5. Returns a 202 Accepted response to the user *immediately*.  
* **Controller's Actions (Asynchronous):**  
  1. A background worker (goroutine) picks up the "queued" job.  
  2. It generates the task.iso from the recipe payload.  
  3. It looks up the BMC for XF-12345ABC and begins the Redfish process (mounts both ISOs, reboots server).  
  4. The worker updates the job status to "provisioning".

---

#### **🌎 GET /api/v1/jobs/{job\_id}**

This allows the user to check the status of a job they started.

* **Action:** Returns the current state of a provisioning job.  
* **Response Body:**  
  JSON  
  {  
      "job\_id": "a1b2c3d4-...",  
      "status": "failed",  
      "failed\_step": "bootloader-linux.service",  
      "server\_serial": "XF-12345ABC",  
      "created\_at": "2025-11-03T20:30:00Z",  
      "last\_update": "2025-11-03T20:35:10Z"  
  }

  *Possible status values: queued, provisioning, succeeded, failed, complete.*

---

### **2\. Internal Webhook API**

This is the internal, non-user-facing API that the systemd services on the bootc-maintenance.iso call to report their final status.

---

#### **🔒 POST /api/v1/status-webhook/{server\_serial}**

This is the endpoint your provision-success.service and provision-failed.service will call.

* **Action:** Receives the final success or failure status from the maintenance OS.  
* **Request Body:**  
  JSON  
  {  
      "status": "success"  
  }

  *...or...*  
  JSON  
  {  
      "status": "failed",  
      "failed\_step": "bootloader-linux.service"  
  }

* **Controller's Actions:**  
  1. Receives the webhook.  
  2. Looks up the *active job* for server\_serial.  
  3. Updates the job's status in the database (e.g., from "provisioning" to "succeeded" or "failed").  
  4. If the status is "succeeded" or "failed", the controller's background worker (which is still monitoring the job) knows it's time to perform cleanup.  
  5. The worker issues the final Redfish commands to unmount all virtual media and reboot the server.  
  6. The worker updates the job status to "complete".  
  7. Returns a 200 OK to the maintenance OS.

---

## **Design Addendum 4.2: Embedded OCI Registry in Controller Service**

### **1\. Overview**

This addendum modifies the v4 design by merging the **Controller Service** and the **OCI Registry** into a single, self-contained Go binary.

Instead of deploying a separate registry service (like zot or distribution/distribution), the Go controller will use the google/go-containerregistry library to natively serve the OCI Distribution API (the /v2/ endpoints) on the same HTTP server that provides the provisioning API (/api/v1/).

This decision simplifies the architecture to its absolute minimum, creating a single service that acts as the "brain," "Redfish client," and "artifact store" for the entire provisioning system.

### **2\. Architectural Modification**

The Controller component is now the single source for all network interactions. The maintenance.iso and CI/CD pipeline no longer talk to two separate services (API and Registry), but only to one.

* **Go Controller Service (e.g., http://10.1.1.10:8080):**  
  * Handles POST /api/v1/jobs (User API)  
  * Handles POST /api/v1/status-webhook/... (Internal Webhook API)  
  * Handles GET /v2/..., PUT /v2/..., etc. (OCI Registry API)

### **3\. Implementation Details**

The implementation is achieved by adding an OCI handler to the Go service's HTTP router. This handler uses an **OCI Layout** (a simple directory on disk) as its storage backend.

#### **1\. Go Controller (main.go \- Conceptual Example)**

Go

package main

import (  
    "log"  
    "net/http"

    // 1\. Import the registry packages  
    "github.com/google/go-containerregistry/pkg/registry"  
    "github.com/google/go-containerregistry/pkg/v1/layout"  
)

// The path on the controller's disk where all artifacts are stored  
const registryStoragePath \= "/var/lib/my-provisioner-registry"

func main() {  
    log.Println("Starting Unified Provisioning Controller...")

    // \--- 2\. OCI Registry Setup \---  
    // Create a "layout" (a directory on disk) to store artifacts  
    layout, err := layout.New(registryStoragePath)  
    if err \!= nil {  
        log.Fatalf("Failed to create OCI layout: %v", err)  
    }

    // Create the OCI registry API handler, using the disk layout as the backend  
    ociHandler := registry.New(registry.WithFallthrough(layout))  
      
    // \--- 3\. API Handler Setup \---  
    apiMux := http.NewServeMux()  
    // User API (for starting jobs)  
    apiMux.HandleFunc("POST /api/v1/jobs", handleProvisionJob)  
    // Internal Webhook API (for status reports)  
    apiMux.HandleFunc("POST /api/v1/status-webhook/{serial}", handleStatusWebhook)  
      
    // \--- 4\. Main Router \---  
    // Create the main router  
    mainMux := http.NewServeMux()  
    // All requests to "/v2/" are sent to the OCI registry handler  
    mainMux.Handle("/v2/", ociHandler)  
    // All other requests are sent to our API handler  
    mainMux.Handle("/", apiMux)

    // Start the single, unified server  
    log.Println("Listening on :8080")  
    if err := http.ListenAndServe(":8080", mainMux); err \!= nil {  
        log.Fatalf("Server failed: %v", err)  
    }  
}

func handleProvisionJob(w http.ResponseWriter, r \*http.Request) {  
    // ... All your logic from v4...  
    // 1\. Get and validate recipe  
    // 2\. Build task.iso  
    // 3\. Make Redfish calls to mount media and reboot  
}

func handleStatusWebhook(w http.ResponseWriter, r \*http.Request) {  
    // ... All your logic from v4...  
    // 1\. Get status from request body  
    // 2\. Update job in database  
    // 3\. Trigger cleanup (unmount/reboot)  
}

#### **2\. CI/CD Pipeline (Pushing Artifacts)**

Your build pipeline now just pushes all artifacts (tool containers, OS images) to the controller's address.

Bash

\# Set the registry URL  
CONTROLLER\_URL="controller.api.internal:8080"

\# Log in (if auth is enabled on the controller)  
oras login $CONTROLLER\_URL \--username user \--password pass

\# Push the Linux filesystem artifact  
oras push $CONTROLLER\_URL/os-images/ubuntu-rootfs:22.04 \\  
  \--artifact-type "application/vnd.my-org.rootfs.tar.gz" \\  
  ./ubuntu-22.04-rootfs.tar.gz

\# Push the Windows WIM artifact  
oras push $CONTROLLER\_URL/os-images/windows-wim:2022 \\  
  \--artifact-type "application/vnd.my-org.install.wim" \\  
  ./win-server-2022.wim

\# Push one of your tool containers  
podman push my-registry.com/tools/sgdisk:1.0 \\  
  $CONTROLLER\_URL/tools/sgdisk:1.0

#### **3\. Maintenance OS (Pulling Artifacts)**

The recipe.json is now even simpler. The oci\_url just points to the controller.

**recipe.json (partial):**

JSON

{  
  "task\_target": "install-linux.target",  
  "oci\_url": "controller.api.internal:8080/os-images/ubuntu-rootfs:22.04"  
  // ...  
}

The oras pull command inside your Quadlet containers will read this URL and work perfectly, as it's just talking to your controller's /v2/ endpoint.

### **4\. Trade-Offs of this Addendum**

* **Pros:**  
  * **Ultimate Simplicity:** The entire system is now a **single binary** (the Go controller) and a **single ISO** (the bootc-maintenance.iso). This is the easiest possible model to deploy, manage, and version.  
  * **Perfect for Air-Gapped:** This design is ideal for an air-gapped or fully self-contained environment. No external dependencies are needed.  
  * **Zero New Services:** You don't need to manage a separate registry, database, or web server.  
* **Cons:**  
  * **No Registry UI:** You lose the web interface that comes with services like Harbor or Zot. Artifacts are managed only via the API (which is all this system needs).  
  * **Coupled Performance:** The controller's API performance is now tied to its disk I/O. If the registry is under heavy load (e.g., pulling a 20GB OS image), API requests for new jobs might be slower. For this use case (a handful of provisioning jobs at a time), this is a non-issue.  
  * **Basic Auth:** You will have to implement your own auth for the registry (e.g., basic auth middleware in Go), whereas dedicated registries have this built-in.

---

## **Provisioning System: Developer & User Guide**

### **1\. Introduction**

This guide provides practical, in-depth examples for building and extending the Layer 3 Bare Metal Provisioner. This system is built on a "dual virtual media" model, where a static bootc-maintenance.iso is paired with a dynamic task.iso to execute isolated, container-based provisioning tasks.

This document covers the three main development activities:

1. **Building the Base bootc-maintenance.iso** (The "Workhorse")  
2. **Adding a New Tool Container** (The "Tools")  
3. **Creating a Full Workflow** (The "Job")

### **2\. Building the Base bootc-maintenance.iso**

The bootc-maintenance.iso is the universal, static OS that boots on every server. It contains the Go dispatcher, systemd, podman, oras, and all your tool container definitions.

#### **Part 2.1: The Go Dispatcher (provisioner)**

This is the single, static Go binary that acts as the entrypoint.

**/cmd/provisioner/main.go:**

Go

package main

import (  
    "encoding/json"  
    "fmt"  
    "log"  
    "os"  
    "os/exec"  
    "syscall"  
    "time"

    "github.com/santhosh-tekuri/jsonschema/v5"  
)

const (  
    taskIsoDevice   \= "/dev/sr1"  
    taskMountPoint  \= "/mnt/task"  
    recipeFile      \= "/mnt/task/recipe.json"  
    schemaFile      \= "/mnt/task/recipe.schema.json"  
    envDir          \= "/run/provision"  
    envFile         \= "/run/provision/recipe.env"  
    layoutFile      \= "/run/provision/layout.json"  
    userDataFile    \= "/run/provision/user-data"  
    unattendFile    \= "/run/provision/unattend.xml"  
)

// This struct will hold all possible recipe values  
type ProvisionRecipe struct {  
    TaskTarget      string          \`json:"task\_target"\`  
    TargetDisk      string          \`json:"target\_disk,omitempty"\`  
    OCIUrl          string          \`json:"oci\_url,omitempty"\`  
    FirmwareUrl     string          \`json:"firmware\_url,omitempty"\`  
    UserData        string          \`json:"user\_data,omitempty"\`  
    UnattendXml     string          \`json:"unattend\_xml,omitempty"\`  
    PartitionLayout json.RawMessage \`json:"partition\_layout,omitempty"\`  
    // Add any new top-level dynamic fields here  
}

func main() {  
    log.Println("Provisioning Dispatcher Started...")

    // 1\. Mount the task ISO  
    if err := mountTaskIso(); err \!= nil {  
        log.Fatalf("Fatal: could not mount task ISO: %v", err)  
    }

    // 2\. Load and validate the recipe.json  
    recipe, rawRecipe, err := validateRecipe()  
    if err \!= nil {  
        log.Fatalf("Fatal: recipe validation failed: %v", err)  
    }  
    log.Println("Recipe is valid.")

    // 3\. Write all recipe data to /run/provision  
    if err := writeRecipeFiles(recipe); err \!= nil {  
        log.Fatalf("Fatal: could not write recipe files: %v", err)  
    }

    // 4\. Start the main systemd target  
    if err := startTask(recipe.TaskTarget); err \!= nil {  
        log.Fatalf("Fatal: failed to start task %s: %v", recipe.TaskTarget, err)  
    }

    log.Printf("Dispatcher finished. Handed off to systemd target: %s", recipe.TaskTarget)  
}

func mountTaskIso() error {  
    log.Printf("Waiting for task ISO at %s...", taskIsoDevice)  
    for {  
        if \_, err := os.Stat(taskIsoDevice); err \== nil {  
            break  
        }  
        time.Sleep(1 \* time.Second)  
    }  
    log.Println("Task ISO found.")

    if err := os.MkdirAll(taskMountPoint, 0755); err \!= nil {  
        return err  
    }  
    // Mount read-only  
    return syscall.Mount(taskIsoDevice, taskMountPoint, "iso9660", syscall.MS\_RDONLY, "")  
}

func validateRecipe() (\*ProvisionRecipe, \[\]byte, error) {  
    // 2a. Load the schema  
    compiler := jsonschema.NewCompiler()  
    schema, err := compiler.Compile(schemaFile)  
    if err \!= nil {  
        return nil, nil, fmt.Errorf("could not compile schema: %v", err)  
    }

    // 2b. Load the recipe  
    recipeData, err := os.ReadFile(recipeFile)  
    if err \!= nil {  
        return nil, nil, fmt.Errorf("could not read recipe: %v", err)  
    }

    // 2c. Validate the recipe against the schema  
    var v interface{}  
    if err := json.Unmarshal(recipeData, \&v); err \!= nil {  
        return nil, nil, fmt.Errorf("recipe is not valid JSON: %v", err)  
    }  
    if err \= schema.Validate(v); err \!= nil {  
        return nil, nil, fmt.Errorf("recipe validation failed: %v", err)  
    }  
      
    // 2d. Unmarshal into our struct  
    var recipe \*ProvisionRecipe  
    if err := json.Unmarshal(recipeData, \&recipe); err \!= nil {  
        return nil, nil, err  
    }  
    return recipe, recipeData, nil  
}

func writeRecipeFiles(recipe \*ProvisionRecipe) error {  
    if err := os.MkdirAll(envDir, 0755); err \!= nil {  
        return err  
    }

    // 3a. Write simple key-value pairs to a .env file  
    var envContent string  
    if recipe.TaskTarget \!= "" {  
        envContent \+= fmt.Sprintf("TASK\_TARGET=%s\\n", recipe.TaskTarget)  
    }  
    if recipe.TargetDisk \!= "" {  
        envContent \+= fmt.Sprintf("TARGET\_DISK=%s\\n", recipe.TargetDisk)  
    }  
    if recipe.OCIUrl \!= "" {  
        envContent \+= fmt.Sprintf("OCI\_URL=%s\\n", recipe.OCIUrl)  
    }  
    if recipe.FirmwareUrl \!= "" {  
        envContent \+= fmt.Sprintf("FIRMWARE\_URL=%s\\n", recipe.FirmwareUrl)  
    }  
    // Note: We also need the server's serial for webhooks  
    // envContent \+= fmt.Sprintf("SERIAL\_NUMBER=%s\\n", getDmiSerial())  
      
    if err := os.WriteFile(envFile, \[\]byte(envContent), 0644); err \!= nil {  
        return err  
    }

    // 3b. Write complex/large files  
    if err := os.WriteFile(layoutFile, recipe.PartitionLayout, 0644); err \!= nil {  
        return err  
    }  
    if err := os.WriteFile(userDataFile, \[\]byte(recipe.UserData), 0644); err \!= nil {  
        return err  
    }  
    if err := os.WriteFile(unattendFile, \[\]byte(recipe.UnattendXml), 0644); err \!= nil {  
        return err  
    }  
      
    log.Println("All recipe files written to /run/provision")  
    return nil  
}

func startTask(taskTarget string) error {  
    cmd := exec.Command("systemctl", "start", taskTarget)  
    cmd.Stdout \= os.Stdout  
    cmd.Stderr \= os.Stderr  
    return cmd.Run()  
}

#### **Part 2.2: The systemd Dispatcher Service**

This service starts the Go binary on boot.

**/systemd/provision-dispatcher.service:**

Ini, TOML

\[Unit\]  
Description\=Provisioning Dispatcher  
DefaultDependencies\=no  
After\=local-fs.target systemd-udev-settle.service  
Wants\=local-fs.target systemd-udev-settle.service

\[Service\]  
Type\=oneshot  
RemainAfterExit\=true  
ExecStart\=/usr/sbin/provisioner  
\# If this Go binary fails, we trigger the failure target  
OnFailure\=provision-failed.target

\[Install\]  
WantedBy\=multi-user.target

*Note: We also need provision-failed.service for this to work, shown in Section 4\.*

#### **Part 2.3: The bootc Containerfile**

This multi-stage Containerfile assembles the final bootc OCI image, which you will then convert into a bootable iso using bootc-image-builder.

**Containerfile:**

Dockerfile

\# \--- Builder Stage \---  
\# Compiles our static Go binary  
FROM golang:1.22\-alpine AS builder

WORKDIR /app  
\# Copy the Go source (and go.mod, etc.)  
COPY . .

\# Build a static, self-contained binary  
RUN CGO\_ENABLED=0 GOOS=linux go build \-ldflags="-w \-s" \-o /provisioner ./cmd/provisioner

\# \--- Final Stage \---  
\# This is our actual maintenance OS  
FROM quay.io/fedora/fedora-bootc:40

\# 1\. Install required host tools  
RUN dnf install \-y oras && dnf clean all

\# 2\. Copy the compiled Go binary from the builder  
COPY \--from=builder /provisioner /usr/sbin/provisioner

\# 3\. Copy all our systemd and Quadlet definitions  
COPY systemd/ /etc/systemd/system/  
COPY quadlets/ /etc/containers/systemd/

\# 4\. Enable the dispatcher service so it runs on boot  
RUN systemctl enable provision-dispatcher.service

\# 5\. Logically bind all tool containers for offline use  
\#    This makes the ISO self-contained.  
\#    (This assumes your Quadlet files are in place)  
RUN mkdir \-p /usr/lib/bootc/bound-images.d && \\  
    ln \-s /etc/containers/systemd/partition.container /usr/lib/bootc/bound-images.d/ && \\  
    ln \-s /etc/containers/systemd/image-linux.container /usr/lib/bootc/bound-images.d/ && \\  
    ln \-s /etc/containers/systemd/image-windows.container /usr/lib/bootc/bound-images.d/ && \\  
    ln \-s /etc/containers/systemd/bootloader-linux.container /usr/lib/bootc/bound-images.d/ && \\  
    ln \-s /etc/containers/systemd/bootloader-windows.container /usr/lib/bootc/bound-images.d/ && \\  
    ln \-s /etc/containers/systemd/config-drive.container /usr/lib/bootc/bound-images.d/  
    \# Add new tool containers here

---

### **3\. Adding a New Tool (Example: wimapply for Windows)**

This is the standard workflow for adding a new tool.

#### **Step 1\. Build the Tool Container**

First, create a container that *just* has the tool and its dependencies.

**tool-containers/wimapply/Containerfile:**

Dockerfile

FROM fedora:40

\# Install wimlib-tools (for wimapply) and ntfs-3g (for formatting)  
RUN dnf install \-y wimlib-tools ntfs-3g && dnf clean all

\# This container will be run as a "command", not a daemon.  
\# We just need the tools in the $PATH.

Build and Push:  
podman build \-t my-registry.com/tools/wimapply:1.0 .  
podman push my-registry.com/tools/wimapply:1.0

#### **Step 2\. Define the Quadlet**

Next, add a .container file to your bootc-maintenance-iso project. This defines *how* systemd runs this tool.

**/quadlets/image-windows.container:**

Ini, TOML

\[Unit\]  
Description\=Quadlet Tool: Windows Imager (wimapply)

\[Container\]  
\# 1\. The image to run (this will be pre-pulled by bootc)  
Image\=my-registry.com/tools/wimapply:1.0  
Type\=oneshot  
Remove\=true

\# 2\. Load the recipe variables (OCI\_URL, TARGET\_DISK)  
EnvironmentFile\=/run/provision/recipe.env

\# 3\. Give it the privileges it needs  
AddDevice\=/dev:/dev:rwm  
AddCapability\=ALL

\# 4\. Define the command to run \*inside\* the container  
\#    This script will:  
\#    a. Read TARGET\_DISK and OCI\_URL from env  
\#    b. Find the main Windows partition on $TARGET\_DISK  
\#    c. Mount it to /mnt/new-windows  
\#    d. \`oras pull $OCI\_URL | wimapply \- /mnt/new-windows \--index=1\`  
Exec\=/usr/bin/wimapply-wrapper.sh

*Note: You would also build a simple wimapply-wrapper.sh script into this container to perform the logic in step 4\.*

---

### **4\. Creating a Full Workflow (Example: Install Linux)**

A "workflow" is just a master systemd target that combines your tools.

#### **Step 1: Define the Master systemd Target**

This file defines the entire "Install Linux" job.

**/systemd/install-linux.target:**

Ini, TOML

\[Unit\]  
Description\=Master Target: Install Traditional Linux  
\# Run these services. If any fail, this target fails.  
Requires\=partition.service  
Requires\=image-linux.service  
Requires\=bootloader-linux.service  
Requires\=config-drive.service

\# Define the order  
After\=partition.service  
After\=image-linux.service  
After\=bootloader-linux.service  
After\=config-drive.service

\# 4\. Call the webhooks  
OnSuccess\=provision-success.service  
OnFailure\=provision-failed.service

*(This assumes you have created partition.container, image-linux.container, bootloader-linux.container, and config-drive.container in your /quadlets directory.)*

#### **Step 2: Define the Feedback Loop**

These services send the final status to your controller.

**/systemd/provision-success.service:**

Ini, TOML

\[Unit\]  
Description\=Report Provisioning Success  
After\=network-online.target  
Wants\=network-online.target

\[Service\]  
Type\=oneshot  
EnvironmentFile\=/run/provision/recipe.env  
\# $SERIAL\_NUMBER would be fetched by the Go binary  
ExecStart\=/usr/bin/curl \-X POST \-d '{"status": "success"}' \\  
  http://controller.api/v1/job-status/${SERIAL\_NUMBER}

**/systemd/provision-failed.service:**

Ini, TOML

\[Unit\]  
Description\=Report Provisioning Failure  
After\=network-online.target  
Wants\=network-online.target

\[Service\]  
Type\=oneshot  
EnvironmentFile\=/run/provision/recipe.env  
\# %n is the service that failed  
ExecStart\=/usr/bin/curl \-X POST \\  
  \-d '{"status": "failed", "failed\_step": "%n"}' \\  
  http://controller.api/v1/job-status/${SERIAL\_NUMBER}

#### **Step 3: Write the User Recipe**

A user (or your controller) now just needs to write this recipe.json and put it in the task.iso. Your Go binary and systemd handle the rest.

**recipe.json (for this job):**

JSON

{  
  "$schema": "./recipe.schema.json",  
  "task\_target": "install-linux.target",  
  "target\_disk": "/dev/sda",  
  "oci\_url": "my-registry.com/os-images/ubuntu-rootfs:22.04",  
  "user\_data": "IyEvYmluL2Jhc2gKaG9zdG5hbWUgLXAgc2VydmVyMDEK...",  
  "partition\_layout": \[  
    {  
      "size": "512M",  
      "type\_guid": "ef00",  
      "format": "vfat"  
    },  
    {  
      "size": "100%",  
      "type\_guid": "8300",  
      "format": "ext4"  
    },  
    {  
      "size": "16M",  
      "type\_guid": "8300",  
      "format": "vfat",  
      "label": "cidata"  
    }  
  \]  
}

