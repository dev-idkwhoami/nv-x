#define _GNU_SOURCE

#include <dlfcn.h>
#include <fcntl.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static const char *redirect_path(const char *path) {
  if (path && strcmp(path, "/etc/os-release") == 0) {
    const char *override = getenv("NV_VCAM_FAKE_OS_RELEASE");
    if (override && override[0] != '\0') {
      return override;
    }
    return "/tmp/nv-vcam-fake-os-release";
  }
  return path;
}

FILE *fopen(const char *path, const char *mode) {
  static FILE *(*real_fopen)(const char *, const char *) = NULL;
  if (!real_fopen) {
    real_fopen = dlsym(RTLD_NEXT, "fopen");
  }
  return real_fopen(redirect_path(path), mode);
}

FILE *fopen64(const char *path, const char *mode) {
  static FILE *(*real_fopen64)(const char *, const char *) = NULL;
  if (!real_fopen64) {
    real_fopen64 = dlsym(RTLD_NEXT, "fopen64");
  }
  return real_fopen64(redirect_path(path), mode);
}

int open(const char *path, int flags, ...) {
  static int (*real_open)(const char *, int, ...) = NULL;
  if (!real_open) {
    real_open = dlsym(RTLD_NEXT, "open");
  }

  if (flags & O_CREAT) {
    va_list ap;
    va_start(ap, flags);
    mode_t mode = (mode_t)va_arg(ap, int);
    va_end(ap);
    return real_open(redirect_path(path), flags, mode);
  }
  return real_open(redirect_path(path), flags);
}

int open64(const char *path, int flags, ...) {
  static int (*real_open64)(const char *, int, ...) = NULL;
  if (!real_open64) {
    real_open64 = dlsym(RTLD_NEXT, "open64");
  }

  if (flags & O_CREAT) {
    va_list ap;
    va_start(ap, flags);
    mode_t mode = (mode_t)va_arg(ap, int);
    va_end(ap);
    return real_open64(redirect_path(path), flags, mode);
  }
  return real_open64(redirect_path(path), flags);
}

int openat(int dirfd, const char *path, int flags, ...) {
  static int (*real_openat)(int, const char *, int, ...) = NULL;
  if (!real_openat) {
    real_openat = dlsym(RTLD_NEXT, "openat");
  }

  if (flags & O_CREAT) {
    va_list ap;
    va_start(ap, flags);
    mode_t mode = (mode_t)va_arg(ap, int);
    va_end(ap);
    return real_openat(dirfd, redirect_path(path), flags, mode);
  }
  return real_openat(dirfd, redirect_path(path), flags);
}
