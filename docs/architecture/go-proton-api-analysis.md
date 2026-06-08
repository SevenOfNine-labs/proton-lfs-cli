# go-proton-api Analysis for proton-lfs-cli

**Date**: 2026-02-16
**Repository**: https://github.com/ProtonMail/go-proton-api
**Status**: Official Proton AG library, actively maintained

## Executive Summary

The `go-proton-api` library provides **limited Proton Drive support** focused on metadata operations. It **does not** provide the file upload/download with end-to-end encryption that we need for Git LFS. **Not recommended** as a replacement for our current proton-drive-cli bridge.

## What go-proton-api Provides

### ✅ Supported Operations

**Volume Management:**
- `ListVolumes()` - Get all available Drive volumes
- `GetVolume(volumeID)` - Get specific volume details

**Link Management:**
- `GetLink(shareID, linkID)` - Get link metadata
- `CreateFile(shareID, req)` - Create file metadata entry
- `CreateFolder(shareID, req)` - Create folder metadata entry

**File Operations:**
- `ListRevisions(shareID, linkID)` - List file revisions
- `GetRevision(shareID, linkID, revisionID, ...)` - Get revision details
- `UpdateRevision(shareID, linkID, revisionID, req)` - Update revision metadata

**Folder Operations:**
- `ListChildren(shareID, linkID, showAll)` - List folder contents
- `TrashChildren(shareID, linkID, childIDs)` - Move children to trash
- `DeleteChildren(shareID, linkID, childIDs)` - Permanently delete children

**Event System:**
- Drive event handling for synchronization (`event_drive.go`)

**Generic Download:**
- `DownloadAndVerify(kr, url, sig)` - Download and verify signed files (not Drive-specific)

### ❌ Missing Critical Features

**No File Content Upload:**
- `CreateFile()` only creates metadata, doesn't upload binary content
- No streaming upload support
- No chunked upload for large files

**No File Content Download:**
- No Drive-specific download methods
- Generic `DownloadAndVerify()` requires URL, not Drive link/revision ID
- Would need to extract download URL from Drive API first

**No End-to-End Encryption:**
- No file content encryption before upload
- No file content decryption after download
- Uses `gopenpgp` for signatures but not for file encryption

**No Complete Workflow:**
- No helper methods for the full upload/download cycle
- No integration between metadata operations and content transfer
- No automatic key management for file encryption

## Comparison with proton-drive-cli

| Feature | go-proton-api | proton-drive-cli | Winner |
|---------|---------------|------------------|--------|
| **Authentication** | ✅ SRP auth | ✅ SRP auth | Tie |
| **Session Management** | ✅ Token refresh | ✅ Token refresh | Tie |
| **Volume Operations** | ✅ List/Get | ✅ Full CRUD | proton-drive-cli |
| **File Upload** | ❌ Metadata only | ✅ Full E2E encrypted | **proton-drive-cli** |
| **File Download** | ❌ No Drive support | ✅ Full E2E decryption | **proton-drive-cli** |
| **Encryption** | ❌ Signatures only | ✅ AES-256 + OpenPGP | **proton-drive-cli** |
| **Official SDK** | ❌ Independent | ✅ Uses @protontech/drive-sdk | **proton-drive-cli** |
| **Language** | ✅ Native Go | ❌ Node.js subprocess | go-proton-api |
| **Documentation** | ❌ Minimal | ✅ Comprehensive | proton-drive-cli |
| **Examples** | ❌ None | ✅ Full CLI + bridge | proton-drive-cli |

## Why We Can't Use It

### 1. Incomplete Drive API Coverage

The library only implements metadata operations. For Git LFS, we need:
```
Upload Flow (Required):
1. Generate encryption keys ❌ Not provided
2. Encrypt file content (AES-256) ❌ Not provided
3. Upload encrypted blocks ❌ Not provided
4. Create file metadata ✅ CreateFile() available
5. Update revision info ✅ UpdateRevision() available

Download Flow (Required):
1. Get file metadata ✅ GetLink() available
2. Get revision info ✅ GetRevision() available
3. Download encrypted blocks ❌ Not provided
4. Decrypt file content ❌ Not provided
```

### 2. No Official Drive SDK Integration

- proton-drive-cli uses `@protontech/drive-sdk` - the **official** Proton Drive SDK
- go-proton-api is independent and may not match official SDK behavior
- Official SDK has guaranteed compatibility with Proton API changes

### 3. Missing E2E Encryption Layer

The library has no file encryption implementation. We would need to:
- Implement AES-256 file encryption (complex)
- Implement OpenPGP key management (complex)
- Implement block-based chunking (complex)
- Match Proton's exact encryption format (very complex, undocumented)

### 4. No Examples or Documentation

- No Drive usage examples in the repository
- No tests demonstrating Drive workflows
- Would require reverse-engineering the protocol

## What We Can Potentially Harvest

### 1. Authentication Flow Reference

**File**: `manager_auth.go`

The SRP authentication implementation could be useful as a reference, but our current proton-drive-cli already handles this correctly.

**Value**: Low - we already have working auth

### 2. API Endpoint Discovery

**Files**: `link.go`, `volume.go`, `link_file.go`, `link_folder.go`

Shows the actual Proton Drive API endpoints:
```go
GET /drive/volumes
GET /drive/volumes/{volumeID}
GET /drive/shares/{shareID}/links/{linkID}
POST /drive/shares/{shareID}/files
POST /drive/shares/{shareID}/folders
GET /drive/shares/{shareID}/links/{linkID}/revisions
// etc.
```

**Value**: Medium - useful for understanding the API structure

### 3. Type Definitions

**Files**: `*_types.go` files

Provides Go struct definitions for API responses:
```go
type Volume struct { ... }
type Link struct { ... }
type Revision struct { ... }
// etc.
```

**Value**: Low - we use TypeScript types from the official SDK

### 4. Error Handling Patterns

**Files**: Throughout codebase

Shows how Proton API errors are handled in Go.

**Value**: Low - our current error handling is adequate

## Recommendations

### ❌ Do Not Replace proton-drive-cli Bridge

**Reasons:**
1. go-proton-api lacks file content upload/download
2. Missing E2E encryption implementation
3. Not based on official Drive SDK
4. Would require 6+ months of development to implement missing features
5. High risk of incompatibility with Proton's encryption format

### ✅ Keep Current Architecture

**Our current approach is better:**
```
Go Adapter → proton-drive-cli (Node.js subprocess) → @protontech/drive-sdk
```

**Advantages:**
- Uses official Drive SDK (guaranteed compatibility)
- Complete E2E encryption implementation
- Full file upload/download support
- Well-tested and working
- Maintained by Proton AG

### 💡 Potential Future Optimization

**Only if** Proton AG publishes an official Go Drive SDK with full E2E encryption, consider replacing the Node.js bridge. Until then, keep the current architecture.

### 📚 Reference Use Only

Use go-proton-api as a **reference** for:
- Understanding Proton Drive API endpoints
- Learning the REST API structure
- Cross-referencing authentication flows

But **do not use** for production Drive operations.

## Conclusion

The go-proton-api library is **not suitable** for our Git LFS use case due to missing file content transfer and encryption features. Our current proton-drive-cli bridge using the official @protontech/drive-sdk is the correct architecture.

**Recommendation**: Keep current implementation, no changes needed.

---

**Analysis performed by**: Claude (Anthropic)
**For project**: proton-lfs-cli v0.1.1
**Repository analyzed**: ProtonMail/go-proton-api @ master
