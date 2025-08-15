# Security Improvements in Config Package

## Path Traversal Prevention

This package has been updated to prevent path traversal attacks by implementing the following security measures:

### 1. Path Validation Function

The `validateConfigPath()` function performs comprehensive validation of file paths:

- **Path Cleaning**: Uses `filepath.Clean()` to resolve `.` and `..` components
- **Absolute Path Resolution**: Converts all paths to absolute paths to prevent relative path issues
- **Traversal Pattern Detection**: Explicitly checks for `..` and `//` patterns
- **Control Character Filtering**: Prevents paths containing null bytes or control characters
- **File Extension Validation**: Restricts file extensions to known configuration formats

### 2. Secure File Operations

All file operations now use validated paths:

- **Config File Reading/Writing**: All `os.ReadFile()` and `os.WriteFile()` operations use validated paths
- **Backup File Creation**: Backup paths are also validated to prevent attacks
- **Directory Creation**: Directory paths are cleaned before `os.MkdirAll()` operations
- **Private Key Loading**: Private key file paths are validated in `postprocessConfig()`

### 3. Defense in Depth

Multiple layers of protection:

- **Input Validation**: All user-provided paths are validated before use
- **Path Canonicalization**: Paths are converted to canonical form
- **Extension Whitelisting**: Only allowed file extensions are permitted
- **Error Handling**: Invalid paths return descriptive errors without exposing system details

## Additional Security Measures

### 4. System Directory Protection

Restricted access to sensitive system directories:
- Blocks access to `/etc/` (except `/etc/yggdrasil/`)
- Blocks access to `/root/`, `/var/` (except `/var/lib/yggdrasil/`)
- Blocks access to `/sys/`, `/proc/`, `/dev/`

### 5. Path Depth Limiting

Maximum path depth of 10 levels to prevent deeply nested attacks.

## Allowed File Extensions

The following file extensions are permitted for configuration files:
- `.json` - JSON configuration files
- `.hjson` - Human JSON configuration files  
- `.conf` - Generic configuration files
- `.config` - Configuration files
- `.yml` - YAML configuration files
- `.yaml` - YAML configuration files
- (no extension) - Files without extensions

## Migration Notes

Existing code using this package should continue to work without changes. Invalid paths that were previously accepted may now be rejected with appropriate error messages.

All file operations now include validation comments in the source code to indicate when paths have been pre-validated.

## Testing

To verify path validation is working correctly:

```go
// Valid paths
validPath, err := validateConfigPath("/etc/yggdrasil/config.json")
// Invalid paths (should return errors)
_, err = validateConfigPath("../../../etc/passwd")
_, err = validateConfigPath("/config/../../../etc/shadow")
```