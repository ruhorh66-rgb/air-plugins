---
id: "N-065_learn-p0-fix6-reject-mixed-separator-win32-devic"
title: "LEARN-P0-FIX6: reject mixed-separator Win32 device aliases"
parent: "N-064_learn-p0-fix5-reject-raw-win32-runtime-aliases-b"
trigger: "Independent gpt-6-astra high review of exact package f324f72c0466c9bf14472487e7383d4e4003d435 returned CHANGES_REQUIRED P2: forward/mixed-separator forms of Win32 device prefixes bypass raw prefix validation and reach filesystem I/O."
owner: "gpt-airworker-handoff-20261007"
done_when: "Windows raw runtime-root device-prefix validation recognizes both slash types and mixed separators without TrimSpace/Clean or filesystem access. Prefix families equivalent to \\\\?\\\\, \\\\.\\\\ and \\\\??\\\\, including //?/, \\\\?/, /?\\?, /??/ and mixed forms, reject fail-closed before runtime/bootstrap/selector/owner mutation. Add regressions that target an existing executable/directory and prove rejection happens before os.Stat/bootstrap I/O. Targeted tests, full regression, exact clean package E2E and independent gpt-6-astra high PASS. Publication/install gates unchanged."
status: "open"
return_to: "N-064_learn-p0-fix5-reject-raw-win32-runtime-aliases-b"
created_at: "2026-10-07T10:32:32.1584987Z"
updated_at: "2026-10-07T10:32:32.1584987Z"
receipts:
---

# LEARN-P0-FIX6: reject mixed-separator Win32 device aliases

- Родитель нити: N-064_learn-p0-fix5-reject-raw-win32-runtime-aliases-b
- Владелец: gpt-airworker-handoff-20261007
- Готово когда: Windows raw runtime-root device-prefix validation recognizes both slash types and mixed separators without TrimSpace/Clean or filesystem access. Prefix families equivalent to \\?\\, \\.\\ and \\??\\, including //?/, \\?/, /?\?, /??/ and mixed forms, reject fail-closed before runtime/bootstrap/selector/owner mutation. Add regressions that target an existing executable/directory and prove rejection happens before os.Stat/bootstrap I/O. Targeted tests, full regression, exact clean package E2E and independent gpt-6-astra high PASS. Publication/install gates unchanged.
