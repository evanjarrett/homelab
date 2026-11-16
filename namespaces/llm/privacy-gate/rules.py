"""Deterministic sensitive-data rules for the privacy gate (stdlib only).

Every rule returns only a CODE, never the matched text: callers log and
persist codes, so nothing here may leak content. A hit on any enabled rule
makes the request sensitive (it stays local).

scan(text) -> sorted list of codes. Variants: the text itself, its
percent-decoded form (only when it contains %XX escapes) and the decoded
form of every base64 run that decodes to mostly printable UTF-8.
"""

from __future__ import annotations

import base64
import binascii
import re
import urllib.parse

# ---------------------------------------------------------------- validators


def luhn_ok(digits: str) -> bool:
    total, alt = 0, False
    for ch in reversed(digits):
        d = ord(ch) - 48
        if alt:
            d *= 2
            if d > 9:
                d -= 9
        total += d
        alt = not alt
    return total % 10 == 0


def iban_ok(candidate: str) -> bool:
    s = candidate.replace(" ", "").upper()
    if not 15 <= len(s) <= 34 or not s[:2].isalpha() or not s[2:4].isdigit():
        return False
    rearranged = s[4:] + s[:4]
    num = []
    for ch in rearranged:
        if ch.isdigit():
            num.append(ch)
        elif "A" <= ch <= "Z":
            num.append(str(ord(ch) - 55))
        else:
            return False
    return int("".join(num)) % 97 == 1


# ---------------------------------------------------------------- patterns

_EMAIL = re.compile(r"(?<![\w.+-])[A-Za-z0-9._%+-]{1,64}@(?:[A-Za-z0-9-]{1,63}\.)+[A-Za-z]{2,24}(?![\w-])")

# NANP with separators, or E.164-style with a leading +. Bare digit runs are
# not phones (timestamps, ids), so a separator or the + is required.
_PHONE = re.compile(
    r"(?<![\w+])(?:\+?1[ .-]?)?(?:\(\d{3}\)\s?|\d{3}[ .-])\d{3}[ .-]\d{4}(?![\w])"
    r"|(?<![\w+])\+\d{1,3}[ .-]?\d{2,4}(?:[ .-]?\d{2,4}){2,4}(?!\w)"
)

# 13-19 digits, optional single space/dash separators, known card prefixes.
_CARD = re.compile(r"(?<![\d-])(?:4|5[1-5]|2[2-7]|3[47]|6(?:011|5))(?:[ -]?\d){11,17}(?![\d-])")

_IBAN = re.compile(r"\b[A-Z]{2}\d{2}(?: ?[A-Z0-9]{4}){2,7}(?: ?[A-Z0-9]{1,4})?\b")

_SSN = re.compile(
    r"(?<![\d-])(?!000|666|9\d\d)\d{3}-(?!00)\d{2}-(?!0000)\d{4}(?![\d-])"
    r"|\b(?:ssn|social security(?: number| no\.?)?)\W{0,4}\d{3}\W?\d{2}\W?\d{4}\b",
    re.I,
)

_STREET_SUFFIX = (
    r"(?:Street|St|Avenue|Ave|Road|Rd|Boulevard|Blvd|Lane|Ln|Drive|Dr|Court|Ct|Way|Place|Pl|"
    r"Terrace|Ter|Circle|Cir|Parkway|Pkwy|Highway|Hwy|Trail|Trl|Square|Sq)"
)
_ADDRESS = re.compile(
    r"\b\d{1,6}\s+(?:[NSEW]\.?\s+)?(?:[A-Z][A-Za-z0-9'-]*\.?\s+){1,4}" + _STREET_SUFFIX + r"\b\.?"
    r"|\bP\.?\s?O\.?\s+Box\s+\d+\b"
    r"|\b(?:Apt|Apartment|Suite|Unit)\.?\s*#?\s*\d+[A-Z]?\b,?\s+[A-Z][a-z]+"
)

_DATE = r"(?:\d{1,4}[-/.]\d{1,2}[-/.]\d{1,4}|(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Sept|Oct|Nov|Dec)[a-z]*\.?\s+\d{1,2},?\s+\d{2,4}|\d{1,2}\s+(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Sept|Oct|Nov|Dec)[a-z]*\.?\s+\d{2,4})"
_DOB = re.compile(r"\b(?:d\.?o\.?b\.?|date of birth|birth ?date|born(?: on)?|birthday)\b\W{0,6}" + _DATE, re.I)

_PRIVATE_IP = re.compile(
    r"(?<![\d.])(?:10\.\d{1,3}|172\.(?:1[6-9]|2\d|3[01])|192\.168)\.\d{1,3}\.\d{1,3}(?![\d.])"
    r"|(?<![:\w])f[cd][0-9a-f]{2}:[0-9a-f:]{2,}",
    re.I,
)

_SECRETS = [
    ("private_key", re.compile(r"-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY(?: BLOCK)?-----")),
    ("jwt", re.compile(r"\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}")),
    ("cloud_key", re.compile(r"\b(?:AKIA|ASIA)[0-9A-Z]{16}\b|\bAIza[0-9A-Za-z_-]{35}\b")),
    ("vcs_token", re.compile(r"\bgh[pousr]_[A-Za-z0-9]{30,}\b|\bgithub_pat_[A-Za-z0-9_]{40,}\b|\bglpat-[A-Za-z0-9_-]{20,}\b")),
    ("api_token", re.compile(
        r"\bsk-(?:ant-|proj-)?[A-Za-z0-9_-]{20,}\b|\bxox[abposr]-[A-Za-z0-9-]{10,}\b"
        r"|\b[rs]k_(?:live|test)_[A-Za-z0-9]{16,}\b|\bhf_[A-Za-z0-9]{30,}\b")),
    ("bearer", re.compile(r"\b(?:Bearer|Basic)\s+[A-Za-z0-9._~+/=-]{20,}")),
    ("url_credentials", re.compile(r"\b[a-z][a-z0-9+.-]{1,15}://[^\s/:@]{1,64}:[^\s/@]{3,128}@[^\s/]+", re.I)),
]

# key = value / key: value with a plausible secret (6+ chars). Placeholders
# ($VAR, <x>, {x}, ****, xxxx, none/null/true/false) are ignored.
_PASSWORD_ASSIGN = re.compile(
    r"""(?ix)\b(?:pass(?:word|wd|phrase)?|pwd|secret|client[_-]?secret|api[_-]?key|access[_-]?key
    |auth[_-]?token|access[_-]?token|refresh[_-]?token|private[_-]?key|token)\b["']?\s*[:=]\s*["']?
    (?P<v>[^\s"',;]{6,})"""
)
_PLACEHOLDER = re.compile(r"^(?:[$<{%]|\*+$|x+$|none$|null$|true$|false$|changeme$|redacted|example|placeholder|your[_-])", re.I)

_MARKER = re.compile(
    r"\b(?:confidential|do not (?:share|forward|distribute)|don'?t (?:share|forward)|"
    r"internal use only|attorney[- ]client|privileged and confidential|under nda|"
    r"not for distribution|strictly private|keep this (?:private|secret|between us))\b",
    re.I,
)

ALL_CODES = (
    "email", "phone", "card", "iban", "ssn", "address", "dob", "private_ip",
    "private_key", "jwt", "cloud_key", "vcs_token", "api_token", "bearer", "url_credentials",
    "password_assign", "marker",
)

# ---------------------------------------------------------------- scanning


def _scan_one(text: str, enabled: frozenset) -> set:
    hits = set()

    def on(code):
        return code in enabled and code not in hits

    if on("email") and _EMAIL.search(text):
        hits.add("email")
    if on("phone") and _PHONE.search(text):
        hits.add("phone")
    if on("card"):
        for m in _CARD.finditer(text):
            digits = re.sub(r"\D", "", m.group(0))
            if 13 <= len(digits) <= 19 and len(set(digits)) > 1 and luhn_ok(digits):
                hits.add("card")
                break
    if on("iban"):
        for m in _IBAN.finditer(text):
            if iban_ok(m.group(0)):
                hits.add("iban")
                break
    if on("ssn") and _SSN.search(text):
        hits.add("ssn")
    if on("address") and _ADDRESS.search(text):
        hits.add("address")
    if on("dob") and _DOB.search(text):
        hits.add("dob")
    if on("private_ip") and _PRIVATE_IP.search(text):
        hits.add("private_ip")
    for code, rx in _SECRETS:
        if on(code) and rx.search(text):
            hits.add(code)
    if on("password_assign"):
        for m in _PASSWORD_ASSIGN.finditer(text):
            if not _PLACEHOLDER.match(m.group("v")):
                hits.add("password_assign")
                break
    if on("marker") and _MARKER.search(text):
        hits.add("marker")
    return hits


_PCT = re.compile(r"%[0-9A-Fa-f]{2}")
_B64 = re.compile(r"(?<![A-Za-z0-9+/_-])[A-Za-z0-9+/_-]{24,}={0,2}")
_MAX_B64_RUNS = 256


def _b64_variants(text: str):
    count = 0
    for m in _B64.finditer(text):
        run = m.group(0)
        if count >= _MAX_B64_RUNS:
            return
        count += 1
        raw = run.replace("-", "+").replace("_", "/")
        raw += "=" * (-len(raw) % 4)
        try:
            data = base64.b64decode(raw, validate=True)
        except (binascii.Error, ValueError):
            continue
        try:
            decoded = data.decode("utf-8")
        except UnicodeDecodeError:
            continue
        printable = sum(1 for c in decoded if c.isprintable() or c in "\r\n\t")
        if decoded and printable / len(decoded) > 0.9:
            yield decoded


def variants(text: str):
    """The text plus decoded views of it (percent-encoding, base64 runs)."""
    yield text
    if _PCT.search(text):
        try:
            dec = urllib.parse.unquote(text)
            if dec != text:
                yield dec
        except Exception:
            pass
    yield from _b64_variants(text)


def scan(text: str, disabled=()) -> list:
    enabled = frozenset(c for c in ALL_CODES if c not in set(disabled))
    hits = set()
    for variant in variants(text):
        hits |= _scan_one(variant, enabled)
        if hits >= enabled:
            break
    return sorted(hits)
