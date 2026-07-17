#!/usr/bin/env python3
"""schemafixer - fix OpenEdge .df schema file area assignments.

Python 3.12 port of the Go `schemafixer` CLI (see cmd/schemafixer in this
repository). Implemented as a single, self-contained file.

Subcommands:
    apply <schema.df> <rules.yaml> [-o OUTPUT]
        Apply area rules from the YAML rules file to a .df schema file.

    parse <schema.df> <rules.yaml> [-o OUTPUT]
        Generate a rules YAML file from an existing .df schema and a
        defaults-only rules file.

    diff <source.df> <target.df> [-o OUTPUT] [--tablemove DATABASE]
        Show area differences between two .df schema files, or (with
        --tablemove) generate `proutil ... -C tablemove` commands.

    flatten <directory|file.df> [file2.df ...] [-o OUTPUT]
        Reset all AREA/LOB-AREA values to "Schema Area" and strip CAN-
        lines. A single directory argument processes all .df files in
        that directory; multiple arguments are treated as explicit files.

Dependencies:
    PyYAML (pip install pyyaml)

Usage examples:
    python schemafixer.py apply schema/sports2020.df model/rules.yaml -o out.df
    python schemafixer.py parse schema/sports2020.df model/default.yaml -o rules.yaml
    python schemafixer.py diff schema/sports2020.df schema/sports2020-prd.df
    python schemafixer.py flatten schema/sports2020.df -o out.df
    python schemafixer.py flatten schema -o flattened
"""

from __future__ import annotations

import argparse
import logging
import os
import re
import sys
from dataclasses import dataclass, field
from typing import Optional

try:
    import yaml
except ImportError:  # pragma: no cover
    sys.stderr.write(
        "error: PyYAML is required. Install it with: pip install pyyaml\n"
    )
    sys.exit(1)

VERSION = "dev"

log = logging.getLogger("schemafixer")

# ── Regular expressions for .df construct detection and area replacement ──
RE_ADD_TABLE = re.compile(r'^ADD TABLE "([^"]+)"', re.IGNORECASE)
RE_ADD_FIELD = re.compile(r'^ADD FIELD "([^"]+)" OF "([^"]+)"', re.IGNORECASE)
RE_ADD_INDEX = re.compile(r'^ADD INDEX "([^"]+)" ON "([^"]+)"', re.IGNORECASE)
RE_ADD_SEQUENCE = re.compile(r'^ADD SEQUENCE ', re.IGNORECASE)
RE_CHECKSUM = re.compile(r'^\d{10}$')
RE_AREA = re.compile(r'^(  AREA ")([^"]+)(".*$)')
RE_LOB_AREA = re.compile(r'^(  LOB-AREA ")([^"]+)(".*$)')

# ── flatten regexes (multiline, ASCII-only patterns) ────────────────────────
RE_FLATTEN_AREA = re.compile(r'^  AREA ".*"$', re.MULTILINE)
RE_FLATTEN_LOB_AREA = re.compile(r'^  LOB-AREA ".*"$', re.MULTILINE)
RE_FLATTEN_CAN = re.compile(r'^  CAN-.*$\n?', re.MULTILINE)

FLATTEN_AREA_REPLACEMENT = '  AREA "Schema Area"'
FLATTEN_LOB_AREA_REPLACEMENT = '  LOB-AREA "Schema Area"'

# ── Parse state ─────────────────────────────────────────────────────────────
STATE_NONE = 0
STATE_TABLE = 1
STATE_FIELD = 2
STATE_INDEX = 3
STATE_OTHER = 4  # sequences and unrecognised constructs — pass through unchanged


# ── Data model (mirrors cmd/schemafixer/commands/models.go) ────────────────
@dataclass
class AreaDefaults:
    table: str = ""
    index: str = ""
    lob: str = ""


@dataclass
class TableRule:
    name: str = ""
    area: str = ""
    indexes: dict[str, str] = field(default_factory=dict)
    lobs: dict[str, str] = field(default_factory=dict)


@dataclass
class SchemaFixerRules:
    version: float = 1.0
    defaults: AreaDefaults = field(default_factory=AreaDefaults)
    tables: list[TableRule] = field(default_factory=list)

    def table_area(self, table_name: str) -> str:
        for t in self.tables:
            if t.name.lower() == table_name.lower() and t.area:
                return t.area
        return self.defaults.table

    def index_area(self, table_name: str, index_name: str) -> str:
        for t in self.tables:
            if t.name.lower() == table_name.lower():
                for k, v in t.indexes.items():
                    if k.lower() == index_name.lower():
                        return v
        return self.defaults.index

    def lob_area(self, table_name: str, field_name: str) -> str:
        for t in self.tables:
            if t.name.lower() == table_name.lower():
                for k, v in t.lobs.items():
                    if k.lower() == field_name.lower():
                        return v
        return self.defaults.lob


# ── Logging ──────────────────────────────────────────────────────────────────
def init_logging(verbose: bool) -> None:
    level = logging.DEBUG if verbose else logging.INFO
    handler = logging.StreamHandler(sys.stderr)
    handler.setFormatter(logging.Formatter("%(levelname)s: %(message)s"))
    log.handlers.clear()
    log.addHandler(handler)
    log.setLevel(level)


# ── File I/O helpers ─────────────────────────────────────────────────────────
def load_rules(path: str) -> SchemaFixerRules:
    with open(path, "r", encoding="utf-8") as f:
        data = yaml.safe_load(f) or {}

    sf = data.get("schemafixer", {}) or {}
    defaults_raw = sf.get("defaults", {}) or {}
    defaults = AreaDefaults(
        table=defaults_raw.get("table", "") or "",
        index=defaults_raw.get("index", "") or "",
        lob=defaults_raw.get("lob", "") or "",
    )

    tables: list[TableRule] = []
    for t in sf.get("tables", []) or []:
        tables.append(
            TableRule(
                name=t.get("name", "") or "",
                area=t.get("area", "") or "",
                indexes={str(k): str(v) for k, v in (t.get("indexes") or {}).items()},
                lobs={str(k): str(v) for k, v in (t.get("lobs") or {}).items()},
            )
        )

    return SchemaFixerRules(
        version=float(sf.get("version", 1.0) or 1.0),
        defaults=defaults,
        tables=tables,
    )


def read_lines(path: str) -> list[str]:
    """Read a file and return its lines with line endings stripped.

    Uses latin-1 to read/write bytes losslessly regardless of the actual
    encoding of the .df file (mirrors the byte-oriented behaviour of the Go
    implementation).
    """
    with open(path, "r", encoding="latin-1", newline="") as f:
        content = f.read()
    return content.splitlines()


# ── apply ─────────────────────────────────────────────────────────────────────
def process_df(lines: list[str], rules: SchemaFixerRules, line_ending: str) -> str:
    state = STATE_NONE
    current_table = current_field = current_index = ""
    out: list[str] = []

    for line in lines:
        m = RE_ADD_TABLE.match(line)
        if m:
            current_table = m.group(1)
            current_field = ""
            current_index = ""
            state = STATE_TABLE
            log.debug("parsing TABLE table=%s", current_table)
        elif (m := RE_ADD_FIELD.match(line)) is not None:
            current_field = m.group(1)
            current_table = m.group(2)
            current_index = ""
            state = STATE_FIELD
            log.debug("parsing FIELD field=%s table=%s", current_field, current_table)
        elif (m := RE_ADD_INDEX.match(line)) is not None:
            current_index = m.group(1)
            current_table = m.group(2)
            current_field = ""
            state = STATE_INDEX
            log.debug("parsing INDEX index=%s table=%s", current_index, current_table)
        elif RE_ADD_SEQUENCE.match(line):
            current_table = current_field = current_index = ""
            state = STATE_OTHER
        elif line.strip() == "":
            state = STATE_NONE

        if state == STATE_TABLE:
            m = RE_AREA.match(line)
            if m:
                area = rules.table_area(current_table)
                line = m.group(1) + area + m.group(3)
                log.debug("TABLE area replaced table=%s area=%s", current_table, area)
        elif state == STATE_INDEX:
            m = RE_AREA.match(line)
            if m:
                area = rules.index_area(current_table, current_index)
                line = m.group(1) + area + m.group(3)
                log.debug(
                    "INDEX area replaced index=%s table=%s area=%s",
                    current_index, current_table, area,
                )
        elif state == STATE_FIELD:
            m = RE_LOB_AREA.match(line)
            if m:
                area = rules.lob_area(current_table, current_field)
                line = m.group(1) + area + m.group(3)
                log.debug(
                    "LOB-AREA replaced field=%s table=%s area=%s",
                    current_field, current_table, area,
                )

        out.append(line)
        out.append(line_ending)

    return "".join(out)


def run_apply(df_path: str, rules_path: str, output_path: Optional[str]) -> int:
    log.debug("apply started df=%s rules=%s output=%s", df_path, rules_path, output_path)

    rules = load_rules(rules_path)
    log.debug(
        "rules loaded tables=%d defaultTable=%s defaultIndex=%s defaultLob=%s",
        len(rules.tables), rules.defaults.table, rules.defaults.index, rules.defaults.lob,
    )

    lines = read_lines(df_path)
    log.debug("df file read lines=%d", len(lines))

    line_ending = "\r\n" if os.name == "nt" else "\n"

    has_checksum = False
    process_lines = lines
    if lines and RE_CHECKSUM.match(lines[-1]):
        has_checksum = True
        process_lines = lines[:-1]
        log.debug("trailing checksum detected -- will recalculate")

    buf = process_df(process_lines, rules, line_ending)

    if output_path:
        with open(output_path, "wb") as f:
            f.write(buf.encode("latin-1"))
            log.debug("writing to file path=%s", output_path)
            if has_checksum:
                byte_count = len(buf.encode("latin-1"))
                checksum_line = f"{byte_count:010d}{line_ending}"
                f.write(checksum_line.encode("latin-1"))
                log.debug("checksum written byteCount=%d", byte_count)
    else:
        sys.stdout.buffer.write(buf.encode("latin-1"))
        if has_checksum:
            byte_count = len(buf.encode("latin-1"))
            checksum_line = f"{byte_count:010d}{line_ending}"
            sys.stdout.buffer.write(checksum_line.encode("latin-1"))
            log.debug("checksum written byteCount=%d", byte_count)

    log.debug("apply complete")
    return 0


# ── parse ─────────────────────────────────────────────────────────────────────
@dataclass
class _TableEntry:
    name: str
    area: str = ""
    indexes: dict[str, str] = field(default_factory=dict)
    lobs: dict[str, str] = field(default_factory=dict)


def run_parse(df_path: str, rules_path: str, output_path: Optional[str]) -> int:
    log.debug("parse started df=%s rules=%s output=%s", df_path, rules_path, output_path)

    rules = load_rules(rules_path)
    defaults = rules.defaults
    log.debug(
        "defaults loaded defaultTable=%s defaultIndex=%s defaultLob=%s",
        defaults.table, defaults.index, defaults.lob,
    )

    lines = read_lines(df_path)
    log.debug("df file read lines=%d", len(lines))

    table_order: list[str] = []  # lower-cased names, insertion order
    table_map: dict[str, _TableEntry] = {}

    def get_or_create(table_name: str) -> _TableEntry:
        key = table_name.lower()
        if key not in table_map:
            table_map[key] = _TableEntry(name=table_name)
            table_order.append(key)
        return table_map[key]

    state = STATE_NONE
    current_table = current_field = current_index = ""

    for line in lines:
        m = RE_ADD_TABLE.match(line)
        if m:
            current_table = m.group(1)
            current_field = ""
            current_index = ""
            state = STATE_TABLE
            log.debug("parsing TABLE table=%s", current_table)
        elif (m := RE_ADD_FIELD.match(line)) is not None:
            current_field = m.group(1)
            current_table = m.group(2)
            current_index = ""
            state = STATE_FIELD
            log.debug("parsing FIELD field=%s table=%s", current_field, current_table)
        elif (m := RE_ADD_INDEX.match(line)) is not None:
            current_index = m.group(1)
            current_table = m.group(2)
            current_field = ""
            state = STATE_INDEX
            log.debug("parsing INDEX index=%s table=%s", current_index, current_table)
        elif RE_ADD_SEQUENCE.match(line):
            current_table = current_field = current_index = ""
            state = STATE_OTHER
        elif line.strip() == "":
            state = STATE_NONE

        if state == STATE_TABLE:
            m = RE_AREA.match(line)
            if m:
                area = m.group(2)
                if area.lower() != defaults.table.lower():
                    e = get_or_create(current_table)
                    e.area = area
                    log.debug("non-default TABLE area table=%s area=%s", current_table, area)
        elif state == STATE_INDEX:
            m = RE_AREA.match(line)
            if m:
                area = m.group(2)
                if area.lower() != defaults.index.lower():
                    e = get_or_create(current_table)
                    e.indexes[current_index.lower()] = area
                    log.debug(
                        "non-default INDEX area index=%s table=%s area=%s",
                        current_index, current_table, area,
                    )
        elif state == STATE_FIELD:
            m = RE_LOB_AREA.match(line)
            if m:
                area = m.group(2)
                if area.lower() != defaults.lob.lower():
                    e = get_or_create(current_table)
                    e.lobs[current_field] = area
                    log.debug(
                        "non-default LOB area field=%s table=%s area=%s",
                        current_field, current_table, area,
                    )

    out_tables = []
    for key in table_order:
        e = table_map[key]
        tr: dict = {"name": e.name, "area": e.area}
        if e.indexes:
            tr["indexes"] = e.indexes
        if e.lobs:
            tr["lobs"] = e.lobs
        out_tables.append(tr)

    out_doc = {
        "schemafixer": {
            "version": rules.version,
            "defaults": {
                "table": defaults.table,
                "index": defaults.index,
                "lob": defaults.lob,
            },
            "tables": out_tables,
        }
    }

    data = yaml.safe_dump(out_doc, sort_keys=False, default_flow_style=False)

    if output_path:
        with open(output_path, "w", encoding="utf-8", newline="") as f:
            f.write(data)
        log.debug("writing to file path=%s", output_path)
    else:
        sys.stdout.write(data)

    log.debug("parse complete tables=%d", len(out_tables))
    return 0


# ── diff ──────────────────────────────────────────────────────────────────────
@dataclass
class AreaRecord:
    construct_type: str  # TABLE, INDEX, LOB
    display_name: str
    key: str
    area: str


@dataclass
class DiffRow:
    construct_type: str
    display_name: str
    source_area: str
    target_area: str


MISSING = "(not present)"


def extract_areas(lines: list[str]) -> list[AreaRecord]:
    records: list[AreaRecord] = []
    state = STATE_NONE
    current_table = current_field = current_index = ""

    for line in lines:
        m = RE_ADD_TABLE.match(line)
        if m:
            current_table = m.group(1)
            current_field = ""
            current_index = ""
            state = STATE_TABLE
        elif (m := RE_ADD_FIELD.match(line)) is not None:
            current_field = m.group(1)
            current_table = m.group(2)
            current_index = ""
            state = STATE_FIELD
        elif (m := RE_ADD_INDEX.match(line)) is not None:
            current_index = m.group(1)
            current_table = m.group(2)
            current_field = ""
            state = STATE_INDEX
        elif RE_ADD_SEQUENCE.match(line):
            current_table = current_field = current_index = ""
            state = STATE_OTHER
        elif line.strip() == "":
            state = STATE_NONE

        if state == STATE_TABLE:
            m = RE_AREA.match(line)
            if m:
                records.append(
                    AreaRecord(
                        construct_type="TABLE",
                        display_name=current_table,
                        key="table:" + current_table.lower(),
                        area=m.group(2),
                    )
                )
        elif state == STATE_INDEX:
            m = RE_AREA.match(line)
            if m:
                records.append(
                    AreaRecord(
                        construct_type="INDEX",
                        display_name=f"{current_table}.{current_index}",
                        key=f"index:{current_table.lower()}.{current_index.lower()}",
                        area=m.group(2),
                    )
                )
        elif state == STATE_FIELD:
            m = RE_LOB_AREA.match(line)
            if m:
                records.append(
                    AreaRecord(
                        construct_type="LOB",
                        display_name=f"{current_table}.{current_field}",
                        key=f"lob:{current_table.lower()}.{current_field.lower()}",
                        area=m.group(2),
                    )
                )

    return records


def quote_if_needed(area: str) -> str:
    if " " in area:
        return f'"{area}"'
    return area


def print_diff_table(w, rows: list[DiffRow]) -> None:
    h_construct, h_name, h_source, h_target = "CONSTRUCT", "NAME", "SOURCE AREA", "TARGET AREA"

    w_construct = len(h_construct)
    w_name = len(h_name)
    w_source = len(h_source)

    for r in rows:
        w_construct = max(w_construct, len(r.construct_type))
        w_name = max(w_name, len(r.display_name))
        w_source = max(w_source, len(r.source_area))

    w_construct += 2
    w_name += 2
    w_source += 2

    def fmt_row(c: str, n: str, s: str, t: str) -> str:
        return f"{c:<{w_construct}}{n:<{w_name}}{s:<{w_source}}{t}\n"

    w.write(fmt_row(h_construct, h_name, h_source, h_target))
    w.write(
        fmt_row(
            "-" * (w_construct - 2),
            "-" * (w_name - 2),
            "-" * (w_source - 2),
            "-" * len(h_target),
        )
    )
    for r in rows:
        w.write(fmt_row(r.construct_type, r.display_name, r.source_area, r.target_area))


def print_proutil_commands(
    w,
    rows: list[DiffRow],
    source_map: dict[str, AreaRecord],
    target_map: dict[str, AreaRecord],
    tablemove_db: str,
) -> None:
    @dataclass
    class TableChange:
        table_name: str
        table_area: str = ""
        index_area: str = ""
        lob_area: str = ""
        has_table: bool = False
        has_index: bool = False
        has_lob: bool = False

    table_changes: dict[str, TableChange] = {}

    for row in rows:
        if row.target_area == MISSING:
            continue

        if row.construct_type == "TABLE":
            table_name = row.display_name
        elif row.construct_type in ("INDEX", "LOB"):
            parts = row.display_name.split(".", 1)
            if len(parts) < 2:
                continue
            table_name = parts[0]
        else:
            continue

        tc = table_changes.setdefault(table_name, TableChange(table_name=table_name))
        if row.construct_type == "TABLE":
            tc.table_area = row.target_area
            tc.has_table = True
        elif row.construct_type == "INDEX":
            tc.index_area = row.target_area
            tc.has_index = True
        elif row.construct_type == "LOB":
            tc.lob_area = row.target_area
            tc.has_lob = True

    for tc in table_changes.values():
        if not (tc.has_table or tc.has_index or tc.has_lob):
            continue

        table_area = tc.table_area
        if not table_area:
            key = "table:" + tc.table_name.lower()
            rec = target_map.get(key) or source_map.get(key)
            if rec:
                table_area = rec.area

        index_area = tc.index_area
        if not index_area and tc.has_index:
            prefix = "index:" + tc.table_name.lower() + "."
            for key, rec in target_map.items():
                if key.startswith(prefix):
                    index_area = rec.area
                    break
        if not index_area:
            prefix = "index:" + tc.table_name.lower() + "."
            for key, rec in source_map.items():
                if key.startswith(prefix):
                    index_area = rec.area
                    break

        lob_area = tc.lob_area
        if not lob_area and tc.has_lob:
            prefix = "lob:" + tc.table_name.lower() + "."
            for key, rec in target_map.items():
                if key.startswith(prefix):
                    lob_area = rec.area
                    break
        if not lob_area and tc.has_lob:
            prefix = "lob:" + tc.table_name.lower() + "."
            for key, rec in source_map.items():
                if key.startswith(prefix):
                    lob_area = rec.area
                    break

        cmd = f"proutil {tablemove_db} -C tablemove {tc.table_name} {quote_if_needed(table_area)}"
        if index_area:
            cmd += " " + quote_if_needed(index_area)
        if lob_area and tc.has_lob:
            cmd += " " + quote_if_needed(lob_area)

        w.write(cmd + "\n")


def run_diff(
    source_path: str, target_path: str, output_path: Optional[str], tablemove_db: str
) -> int:
    log.debug(
        "diff started source=%s target=%s output=%s tablemove=%s",
        source_path, target_path, output_path, tablemove_db,
    )

    source_lines = read_lines(source_path)
    target_lines = read_lines(target_path)

    source_records = extract_areas(source_lines)
    target_records = extract_areas(target_lines)
    log.debug(
        "areas extracted sourceConstructs=%d targetConstructs=%d",
        len(source_records), len(target_records),
    )

    source_map = {r.key: r for r in source_records}
    target_map = {r.key: r for r in target_records}

    rows: list[DiffRow] = []
    seen_keys: set[str] = set()

    for rec in source_records:
        seen_keys.add(rec.key)
        tgt = target_map.get(rec.key)
        if tgt is None:
            rows.append(DiffRow(rec.construct_type, rec.display_name, rec.area, MISSING))
            continue
        if rec.area.lower() != tgt.area.lower():
            rows.append(DiffRow(rec.construct_type, rec.display_name, rec.area, tgt.area))

    for rec in target_records:
        if rec.key not in seen_keys:
            rows.append(DiffRow(rec.construct_type, rec.display_name, MISSING, rec.area))

    if not rows:
        return 0

    close_file = False
    if output_path:
        out = open(output_path, "w", encoding="utf-8", newline="")
        close_file = True
    else:
        out = sys.stdout

    try:
        if tablemove_db:
            print_proutil_commands(out, rows, source_map, target_map, tablemove_db)
        else:
            print_diff_table(out, rows)
    finally:
        if close_file:
            out.close()

    log.debug("diff complete differences=%d", len(rows))
    return 0


# ── flatten ───────────────────────────────────────────────────────────────────
def flatten_file(src_path: str, dest_path: str) -> tuple[int, int, int]:
    """Apply the flatten transformations to src_path and write the result to
    dest_path.

    The .df trailer declares its own codepage via a "cpstream=<name>" line,
    so no encoding assumption is made here. The file is treated as a raw
    byte sequence (via latin-1, same convention as read_lines) since
    AREA/LOB-AREA/CAN- constructs are always plain ASCII; any multi-byte
    payload elsewhere in the file (descriptions, labels, etc.) is passed
    through untouched regardless of its actual codepage.
    """
    log.debug("processing file=%s", src_path)

    with open(src_path, "r", encoding="latin-1", newline="") as f:
        raw = f.read()

    # Normalize to LF for regex processing, then restore platform line
    # endings on write.
    content = raw.replace("\r\n", "\n")

    area_count = len(RE_FLATTEN_AREA.findall(content))
    lob_area_count = len(RE_FLATTEN_LOB_AREA.findall(content))
    can_count = len(RE_FLATTEN_CAN.findall(content))

    new_content = RE_FLATTEN_AREA.sub(FLATTEN_AREA_REPLACEMENT, content)
    new_content = RE_FLATTEN_LOB_AREA.sub(FLATTEN_LOB_AREA_REPLACEMENT, new_content)
    new_content = RE_FLATTEN_CAN.sub("", new_content)

    line_ending = "\r\n" if os.name == "nt" else "\n"
    if line_ending != "\n":
        new_content = new_content.replace("\n", line_ending)

    with open(dest_path, "w", encoding="latin-1", newline="") as f:
        f.write(new_content)

    log.info(
        "flattened file=%s area=%d lobArea=%d canDeleted=%d",
        os.path.basename(src_path), area_count, lob_area_count, can_count,
    )

    return area_count, lob_area_count, can_count


def run_flatten(paths: list[str], output_path: Optional[str]) -> int:
    files: list[str] = []
    dir_mode = False

    if len(paths) == 1 and os.path.isdir(paths[0]):
        dir_mode = True
        for name in sorted(os.listdir(paths[0])):
            full = os.path.join(paths[0], name)
            if os.path.isfile(full) and name.lower().endswith(".df"):
                files.append(full)
    else:
        files = paths

    if output_path:
        if dir_mode:
            os.makedirs(output_path, exist_ok=True)
        elif len(files) > 1:
            log.error(
                "--output cannot be used with multiple file arguments; "
                "specify a single file or a directory"
            )
            return 1

    for path in files:
        if output_path is None:
            dest = path
        elif dir_mode:
            dest = os.path.join(output_path, os.path.basename(path))
        else:
            dest = output_path

        flatten_file(path, dest)

    return 0


# ── CLI ───────────────────────────────────────────────────────────────────────
def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="schemafixer",
        description="Fix OpenEdge .df schema file area assignments",
    )
    parser.add_argument(
        "--version", action="version", version=f"schemafixer {VERSION}"
    )
    parser.add_argument(
        "-v", "--verbose", action="store_true", help="Enable verbose (debug) logging"
    )

    subparsers = parser.add_subparsers(dest="command")

    apply_cmd = subparsers.add_parser(
        "apply", help="Apply area rules to a .df schema file"
    )
    apply_cmd.add_argument("df", help="Input .df schema file")
    apply_cmd.add_argument("rules", help="Rules YAML file")
    apply_cmd.add_argument(
        "-o", "--output", help="Write output to file instead of stdout"
    )

    parse_cmd = subparsers.add_parser(
        "parse", help="Generate a rules file from an existing .df schema"
    )
    parse_cmd.add_argument("df", help="Input .df schema file")
    parse_cmd.add_argument("rules", help="Rules YAML file with defaults")
    parse_cmd.add_argument(
        "-o", "--output", help="Write output to file instead of stdout"
    )

    diff_cmd = subparsers.add_parser(
        "diff", help="Show area differences between two .df schema files"
    )
    diff_cmd.add_argument("source", help="Source .df schema file")
    diff_cmd.add_argument("target", help="Target .df schema file")
    diff_cmd.add_argument(
        "-o", "--output", help="Write output to file instead of stdout"
    )
    diff_cmd.add_argument(
        "--tablemove",
        default="",
        metavar="DATABASE",
        help="Generate proutil tablemove commands for the specified database",
    )

    flatten_cmd = subparsers.add_parser(
        "flatten",
        help='Reset all AREA/LOB-AREA values to "Schema Area" and strip CAN- lines',
    )
    flatten_cmd.add_argument(
        "paths", nargs="+", metavar="directory|file.df",
        help="A directory of .df files, or one or more explicit .df files",
    )
    flatten_cmd.add_argument(
        "-o", "--output",
        help="Write result to this file/directory instead of overwriting in "
             "place (single input: file path; directory input: output directory)",
    )

    return parser


def main(argv: Optional[list[str]] = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)

    init_logging(getattr(args, "verbose", False))

    if not args.command:
        parser.print_help()
        return 1

    try:
        if args.command == "apply":
            return run_apply(args.df, args.rules, args.output)
        elif args.command == "parse":
            return run_parse(args.df, args.rules, args.output)
        elif args.command == "diff":
            return run_diff(args.source, args.target, args.output, args.tablemove)
        elif args.command == "flatten":
            return run_flatten(args.paths, args.output)
        else:
            parser.print_help()
            return 1
    except Exception as exc:  # noqa: BLE001 - top-level CLI error handler
        log.error("fatal error: %s", exc)
        return 1


if __name__ == "__main__":
    sys.exit(main())
