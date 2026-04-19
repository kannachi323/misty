// Misty.app launcher.
//
// Replaces the shell-script CFBundleExecutable with a tiny native binary
// so the bundle passes Apple notarization cleanly. Does the same three
// things the bash launcher did:
//
//   1. Start misty-proxy in the background (detached, logs to
//      ~/Library/Logs/Misty/misty-proxy.log). Skipped if the proxy port
//      is already bound, so a manually-started proxy stays authoritative.
//   2. chdir into Contents/Resources so misty-bin's relative asset
//      lookups (assets/fonts/..., assets/logo/..., imgui.ini) resolve
//      regardless of how the app was launched.
//   3. exec misty-bin, passing argv through.
//
// Keep this file small and boring — it runs every time the app starts.
// If you need more logic, add it to misty-bin, not here.

#include <arpa/inet.h>
#include <errno.h>
#include <fcntl.h>
#include <libgen.h>
#include <limits.h>
#include <mach-o/dyld.h>
#include <netinet/in.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

static int port_listening(int port) {
    int fd = socket(AF_INET, SOCK_STREAM, 0);
    if (fd < 0) return 0;
    struct sockaddr_in addr = {0};
    addr.sin_family = AF_INET;
    addr.sin_port = htons((uint16_t)port);
    inet_pton(AF_INET, "127.0.0.1", &addr.sin_addr);
    int r = connect(fd, (struct sockaddr *)&addr, sizeof(addr));
    close(fd);
    return r == 0;
}

// Read the port from a `KEY=scheme://host:port/...` line in the env file.
// Returns 0 if not found or unparseable.
static int read_port_from_env(const char *path, const char *key) {
    FILE *f = fopen(path, "r");
    if (!f) return 0;

    char line[1024];
    size_t keylen = strlen(key);
    int port = 0;

    while (fgets(line, sizeof(line), f)) {
        char *p = line;
        while (*p == ' ' || *p == '\t') p++;
        if (*p == '#' || *p == '\n' || *p == '\0') continue;
        if (strncmp(p, key, keylen) != 0) continue;
        if (p[keylen] != '=' && p[keylen] != ' ' && p[keylen] != '\t') continue;

        char *eq = strchr(p, '=');
        if (!eq) continue;
        eq++;
        while (*eq == ' ' || *eq == '\t' || *eq == '"' || *eq == '\'') eq++;

        char *nl = strpbrk(eq, "\r\n\"'");
        if (nl) *nl = '\0';

        char *colon = strrchr(eq, ':');
        if (colon) {
            char *slash = strchr(colon, '/');
            if (slash) *slash = '\0';
            port = atoi(colon + 1);
        }
        break;
    }

    fclose(f);
    return port;
}

static void ensure_dir(const char *path) {
    // mkdir returns -1/EEXIST if it already exists; we don't care.
    mkdir(path, 0755);
}

static void spawn_proxy(const char *macos_dir, const char *log_dir) {
    pid_t pid = fork();
    if (pid < 0) return;
    if (pid != 0) return; // parent returns, child continues below

    // Detach from the controlling terminal / session so the proxy survives
    // misty-bin exiting.
    setsid();

    // Redirect stdio to the log file.
    char log_path[PATH_MAX];
    snprintf(log_path, sizeof(log_path), "%s/misty-proxy.log", log_dir);
    int out_fd = open(log_path, O_WRONLY | O_CREAT | O_APPEND, 0644);
    if (out_fd >= 0) {
        dup2(out_fd, STDOUT_FILENO);
        dup2(out_fd, STDERR_FILENO);
        if (out_fd > 2) close(out_fd);
    }
    int null_fd = open("/dev/null", O_RDONLY);
    if (null_fd >= 0) {
        dup2(null_fd, STDIN_FILENO);
        if (null_fd > 2) close(null_fd);
    }

    // Run the proxy with Contents/MacOS as cwd — matches the working dir
    // the bash launcher used.
    chdir(macos_dir);

    char proxy_bin[PATH_MAX];
    snprintf(proxy_bin, sizeof(proxy_bin), "%s/misty-proxy", macos_dir);
    execl(proxy_bin, "misty-proxy", (char *)NULL);
    _exit(127);
}

int main(int argc, char **argv) {
    (void)argc; // argv[0] is the only one we care about

    // Resolve our own path → Contents/MacOS/Misty
    char exe_raw[PATH_MAX];
    uint32_t size = sizeof(exe_raw);
    if (_NSGetExecutablePath(exe_raw, &size) != 0) {
        fprintf(stderr, "launcher: failed to resolve executable path\n");
        return 1;
    }
    char exe_resolved[PATH_MAX];
    if (!realpath(exe_raw, exe_resolved)) {
        strncpy(exe_resolved, exe_raw, sizeof(exe_resolved) - 1);
        exe_resolved[sizeof(exe_resolved) - 1] = '\0';
    }

    // dirname() may modify its argument; give it a throwaway copy.
    char dir_tmp[PATH_MAX];
    strncpy(dir_tmp, exe_resolved, sizeof(dir_tmp) - 1);
    dir_tmp[sizeof(dir_tmp) - 1] = '\0';
    char macos_dir[PATH_MAX];
    strncpy(macos_dir, dirname(dir_tmp), sizeof(macos_dir) - 1);
    macos_dir[sizeof(macos_dir) - 1] = '\0';

    char resources_dir[PATH_MAX];
    snprintf(resources_dir, sizeof(resources_dir), "%s/../Resources", macos_dir);

    char env_file[PATH_MAX];
    snprintf(env_file, sizeof(env_file), "%s/assets/misty.env", resources_dir);

    // Log dir = $HOME/Library/Logs/Misty (create if missing).
    const char *home = getenv("HOME");
    if (!home || !*home) home = "/tmp";
    char library_dir[PATH_MAX], library_logs[PATH_MAX], log_dir[PATH_MAX];
    snprintf(library_dir,  sizeof(library_dir),  "%s/Library",         home);
    snprintf(library_logs, sizeof(library_logs), "%s/Library/Logs",    home);
    snprintf(log_dir,      sizeof(log_dir),      "%s/Library/Logs/Misty", home);
    ensure_dir(library_dir);
    ensure_dir(library_logs);
    ensure_dir(log_dir);

    // Stop misty-bin from trying to auto-start its own proxy — that's our job.
    setenv("MISTY_DISABLE_PROXY_AUTOSTART", "1", 1);

    int proxy_port = read_port_from_env(env_file, "PROXY_SERVICE_URL");
    if (proxy_port > 0 && !port_listening(proxy_port)) {
        spawn_proxy(macos_dir, log_dir);
    }

    // chdir so misty-bin's relative asset paths (assets/logo/..., imgui.ini)
    // resolve against Contents/Resources/ regardless of launch context.
    if (chdir(resources_dir) != 0) {
        fprintf(stderr, "launcher: chdir(%s) failed: %s\n", resources_dir, strerror(errno));
    }

    char misty_bin[PATH_MAX];
    snprintf(misty_bin, sizeof(misty_bin), "%s/misty-bin", macos_dir);
    // argv[0] convention: bare program name.
    argv[0] = (char *)"misty-bin";
    execv(misty_bin, argv);

    fprintf(stderr, "launcher: exec %s failed: %s\n", misty_bin, strerror(errno));
    return 127;
}
