// i18n.js — UI chrome strings only (buttons, banners, debug panel labels).
//
// Field labels, titles, help text and validation messages all come from the
// server already resolved to the session locale (contract.md §3 / plan.md
// "Localization") — the client never translates those. This file only covers
// the small amount of text that belongs to the renderer shell itself, so a
// user can flip the locale switcher and see something change immediately
// even before the next server round trip lands.

const STRINGS = {
  en: {
    appName: "Registration",
    next: "Next",
    back: "Back",
    submit: "Submit",
    accept: "Accept & continue",
    loading: "Loading…",
    retry: "Try again",
    startOver: "Start over",
    resuming: "Resuming your session…",
    complete_title: "You're all set",
    complete_body:
      "We've received everything we need. Verification runs in the background — you'll be notified when it's done.",
    checkStatus: "Check status",
    maintenance_title: "Down for maintenance",
    maintenance_body: "We'll be back shortly. Thanks for your patience.",
    maintenance_retry_in: (s) => `Retrying in ${s}s…`,
    offline_title: "Can't reach the server",
    offline_body: "Check your connection and try again.",
    scroll_to_continue: "Scroll to the bottom to continue",
    doc_stale: "This document was updated while you were reading it. Please review the new version.",
    field_required: "This field is required.",
    upload_take_photo: "Choose photo",
    upload_uploading: "Uploading…",
    upload_retake: "Retake",
    signature_clear: "Clear",
    signature_pad_hint: "Sign above using your mouse or finger",
    pin_label: "Choose a 6-digit PIN",
    pin_confirm_label: "Confirm PIN",
    pin_mismatch: "PINs don't match.",
    external_open: "Open verification",
    external_done: "I've completed this",
    force_update_title: "Update required",
    force_update_body: (v) => `Please update the app to version ${v} or later to continue.`,
    banner_dismiss: "Dismiss",
    debug_title: "Debug",
    debug_api_base: "API base URL",
    debug_locale: "Locale",
    debug_drop_off: "Simulate drop-off & resume",
    debug_forget: "Forget session (start over)",
    debug_last_envelope: "Last server envelope",
    progress_label: (c, t) => `Step ${c} of ${t}`,
    repairs_title: "A few things changed since you left",
    repair_reaccept_document: "The terms & conditions were updated — please review and re-accept.",
    repair_collect_fields: "We need a couple more details before you can continue.",
    repair_redo_step: "We need you to redo this step.",
  },
  id: {
    appName: "Pendaftaran",
    next: "Lanjut",
    back: "Kembali",
    submit: "Kirim",
    accept: "Setuju & lanjutkan",
    loading: "Memuat…",
    retry: "Coba lagi",
    startOver: "Mulai ulang",
    resuming: "Melanjutkan sesi Anda…",
    complete_title: "Semua sudah lengkap",
    complete_body:
      "Kami telah menerima semua yang dibutuhkan. Verifikasi berjalan di latar belakang — Anda akan diberi tahu setelah selesai.",
    checkStatus: "Periksa status",
    maintenance_title: "Sedang dalam pemeliharaan",
    maintenance_body: "Kami akan segera kembali. Terima kasih atas kesabarannya.",
    maintenance_retry_in: (s) => `Mencoba lagi dalam ${s} detik…`,
    offline_title: "Tidak dapat terhubung ke server",
    offline_body: "Periksa koneksi Anda dan coba lagi.",
    scroll_to_continue: "Gulir ke bawah untuk melanjutkan",
    doc_stale: "Dokumen ini diperbarui saat Anda membacanya. Silakan tinjau versi baru.",
    field_required: "Kolom ini wajib diisi.",
    upload_take_photo: "Pilih foto",
    upload_uploading: "Mengunggah…",
    upload_retake: "Ambil ulang",
    signature_clear: "Hapus",
    signature_pad_hint: "Tanda tangan di atas menggunakan mouse atau jari",
    pin_label: "Pilih PIN 6 digit",
    pin_confirm_label: "Konfirmasi PIN",
    pin_mismatch: "PIN tidak cocok.",
    external_open: "Buka verifikasi",
    external_done: "Saya sudah menyelesaikan ini",
    force_update_title: "Pembaruan diperlukan",
    force_update_body: (v) => `Silakan perbarui aplikasi ke versi ${v} atau lebih baru untuk melanjutkan.`,
    banner_dismiss: "Tutup",
    debug_title: "Debug",
    debug_api_base: "URL dasar API",
    debug_locale: "Bahasa",
    debug_drop_off: "Simulasikan putus & lanjutkan",
    debug_forget: "Lupakan sesi (mulai ulang)",
    debug_last_envelope: "Amplop server terakhir",
    progress_label: (c, t) => `Langkah ${c} dari ${t}`,
    repairs_title: "Beberapa hal berubah sejak Anda pergi",
    repair_reaccept_document: "Syarat & ketentuan diperbarui — mohon tinjau dan setujui ulang.",
    repair_collect_fields: "Kami memerlukan beberapa detail tambahan sebelum Anda dapat melanjutkan.",
    repair_redo_step: "Kami perlu Anda mengulangi langkah ini.",
  },
};

// Maps the full BCP-47-ish locale the API speaks (en-US / id-ID) to the
// 2-letter UI-chrome bundle above.
function chromeLocale(locale) {
  const short = (locale || "en").slice(0, 2).toLowerCase();
  return STRINGS[short] ? short : "en";
}

export function t(key, locale, ...args) {
  const bundle = STRINGS[chromeLocale(locale)];
  const entry = bundle[key] ?? STRINGS.en[key];
  if (typeof entry === "function") return entry(...args);
  return entry;
}

export { chromeLocale };
