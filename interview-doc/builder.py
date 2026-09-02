# -*- coding: utf-8 -*-
"""直播弹幕面试题库 Word 渲染引擎"""
import importlib
from docx import Document
from docx.shared import Pt, Cm, RGBColor
from docx.enum.text import WD_ALIGN_PARAGRAPH, WD_BREAK
from docx.enum.section import WD_SECTION
from docx.enum.table import WD_TABLE_ALIGNMENT, WD_ALIGN_VERTICAL
from docx.oxml.ns import qn
from docx.oxml import OxmlElement

BLACK = RGBColor(0, 0, 0)
CODE_FILL = "F2F2F2"
HEAD_FILL = "D9D9D9"
NOTE_FILL = "FBF3E8"


def set_font(run, ascii_f="Arial", ea="宋体", size=12, bold=False, color=BLACK, italic=False):
    run.font.size = Pt(size)
    run.font.bold = bold
    run.font.italic = italic
    run.font.color.rgb = color
    rpr = run._element.get_or_add_rPr()
    rfonts = rpr.find(qn("w:rFonts"))
    if rfonts is None:
        rfonts = OxmlElement("w:rFonts")
        rpr.append(rfonts)
    rfonts.set(qn("w:ascii"), ascii_f)
    rfonts.set(qn("w:hAnsi"), ascii_f)
    rfonts.set(qn("w:eastAsia"), ea)


def shade_par(par, fill):
    ppr = par._p.get_or_add_pPr()
    shd = OxmlElement("w:shd")
    shd.set(qn("w:val"), "clear")
    shd.set(qn("w:color"), "auto")
    shd.set(qn("w:fill"), fill)
    ppr.append(shd)


def first_line_chars(par, chars=200, twips=480):
    ppr = par._p.get_or_add_pPr()
    ind = ppr.find(qn("w:ind"))
    if ind is None:
        ind = OxmlElement("w:ind")
        ppr.append(ind)
    ind.set(qn("w:firstLineChars"), str(chars))
    ind.set(qn("w:firstLine"), str(twips))


def no_indent(par):
    ppr = par._p.get_or_add_pPr()
    ind = ppr.find(qn("w:ind"))
    if ind is None:
        ind = OxmlElement("w:ind")
        ppr.append(ind)
    ind.set(qn("w:firstLineChars"), "0")
    ind.set(qn("w:firstLine"), "0")
    ind.set(qn("w:left"), "0")
    ind.set(qn("w:leftChars"), "0")


def configure_styles(doc):
    # Normal
    normal = doc.styles["Normal"]
    normal.font.name = "Arial"
    normal.font.size = Pt(12)
    normal.font.color.rgb = BLACK
    rpr = normal.element.get_or_add_rPr()
    rf = rpr.get_or_add_rFonts()
    rf.set(qn("w:ascii"), "Arial")
    rf.set(qn("w:hAnsi"), "Arial")
    rf.set(qn("w:eastAsia"), "宋体")
    pf = normal.paragraph_format
    pf.line_spacing = 1.5
    pf.space_before = Pt(0)
    pf.space_after = Pt(0)

    head_conf = [("Heading 1", 16, 14, 6), ("Heading 2", 14, 11, 5), ("Heading 3", 12, 9, 4)]
    for name, size, before, after in head_conf:
        st = doc.styles[name]
        st.font.name = "Arial"
        st.font.size = Pt(size)
        st.font.bold = True
        st.font.color.rgb = BLACK
        r = st.element.get_or_add_rPr()
        rfonts = r.get_or_add_rFonts()
        rfonts.set(qn("w:ascii"), "Arial")
        rfonts.set(qn("w:hAnsi"), "Arial")
        rfonts.set(qn("w:eastAsia"), "黑体")
        st.paragraph_format.space_before = Pt(before)
        st.paragraph_format.space_after = Pt(after)
        st.paragraph_format.line_spacing = 1.3
        keep = OxmlElement("w:keepNext")
        st.element.get_or_add_pPr().append(keep)

    if "Question" not in [s.name for s in doc.styles]:
        qs = doc.styles.add_style("Question", 1)  # paragraph
        qs.base_style = doc.styles["Normal"]
        qs.font.bold = True
        qs.font.size = Pt(12)
        qs.paragraph_format.space_before = Pt(8)
        qs.paragraph_format.space_after = Pt(2)
        qs.paragraph_format.line_spacing = 1.4
    if "Answer" not in [s.name for s in doc.styles]:
        ans = doc.styles.add_style("Answer", 1)
        ans.base_style = doc.styles["Normal"]
        ans.font.size = Pt(12)
        ans.paragraph_format.space_after = Pt(2)
        ans.paragraph_format.line_spacing = 1.5
    if "CodeBlock" not in [s.name for s in doc.styles]:
        cs = doc.styles.add_style("CodeBlock", 1)
        cs.base_style = doc.styles["Normal"]
        cs.font.size = Pt(9.5)
        cs.paragraph_format.line_spacing = 1.15
        cs.paragraph_format.space_before = Pt(2)
        cs.paragraph_format.space_after = Pt(6)
        cs.paragraph_format.left_indent = Cm(0.3)
    if "NoteText" not in [s.name for s in doc.styles]:
        nt = doc.styles.add_style("NoteText", 1)
        nt.base_style = doc.styles["Normal"]
        nt.font.size = Pt(11)
        nt.paragraph_format.line_spacing = 1.4
        nt.paragraph_format.space_before = Pt(3)
        nt.paragraph_format.space_after = Pt(6)
        nt.paragraph_format.left_indent = Cm(0.3)

    # 压缩目录样式，保证目录在一页内
    for toc_name, toc_size, toc_indent in [("TOC 1", 11, 0), ("TOC 2", 10.5, 0.6)]:
        try:
            tst = doc.styles[toc_name]
        except KeyError:
            continue
        tst.font.size = Pt(toc_size)
        tst.font.color.rgb = BLACK
        tst.paragraph_format.line_spacing = 1.0
        tst.paragraph_format.space_before = Pt(1)
        tst.paragraph_format.space_after = Pt(1)
        tst.paragraph_format.left_indent = Cm(toc_indent)
        rr = tst.element.get_or_add_rPr().get_or_add_rFonts()
        rr.set(qn("w:eastAsia"), "宋体")


def add_field(par, instr, placeholder=""):
    run = par.add_run()
    b = OxmlElement("w:fldChar"); b.set(qn("w:fldCharType"), "begin")
    it = OxmlElement("w:instrText"); it.set(qn("xml:space"), "preserve"); it.text = instr
    sep = OxmlElement("w:fldChar"); sep.set(qn("w:fldCharType"), "separate")
    t = OxmlElement("w:t"); t.text = placeholder
    e = OxmlElement("w:fldChar"); e.set(qn("w:fldCharType"), "end")
    for el in (b, it, sep, t, e):
        run._r.append(el)
    return run


def add_toc(doc):
    p = doc.add_paragraph()
    add_field(p, 'TOC \\o "1-2" \\h \\z \\u', "打开文档后右键此处选择“更新域”即可生成目录")


def enable_update_fields(doc):
    el = OxmlElement("w:updateFields")
    el.set(qn("w:val"), "true")
    doc.settings.element.append(el)


def add_footer_pagenum(doc):
    for sec in doc.sections:
        fp = sec.footer.paragraphs[0]
        fp.alignment = WD_ALIGN_PARAGRAPH.CENTER
        r = add_field(fp, "PAGE", "1")
        set_font(r, "Arial", "宋体", 10.5)


def render_block(doc, block):
    kind = block[0]
    if kind == "h1":
        p = doc.add_paragraph(block[1], style="Heading 1")
    elif kind == "h2":
        p = doc.add_paragraph(block[1], style="Heading 2")
    elif kind == "h3":
        p = doc.add_paragraph(block[1], style="Heading 3")
    elif kind == "p":
        p = doc.add_paragraph(style="Normal")
        run = p.add_run(block[1])
        set_font(run, "Arial", "宋体", 12)
        first_line_chars(p)
    elif kind == "q":
        p = doc.add_paragraph(style="Question")
        no_indent(p)
        run = p.add_run(block[1])
        set_font(run, "Arial", "黑体", 12, bold=True)
    elif kind == "a":
        p = doc.add_paragraph(style="Answer")
        no_indent(p)
        lead = p.add_run("【参考答案】")
        set_font(lead, "Arial", "黑体", 12, bold=True)
        run = p.add_run(block[1])
        set_font(run, "Arial", "宋体", 12)
    elif kind == "akey":  # 带加粗引导词的答案：block = ('akey', lead, rest)
        p = doc.add_paragraph(style="Answer")
        no_indent(p)
        r1 = p.add_run("【参考答案】")
        set_font(r1, "Arial", "黑体", 12, bold=True)
        r2 = p.add_run(block[1])
        set_font(r2, "Arial", "黑体", 12, bold=True)
        r3 = p.add_run(block[2])
        set_font(r3, "Arial", "宋体", 12)
    elif kind == "code":
        p = doc.add_paragraph(style="CodeBlock")
        shade_par(p, CODE_FILL)
        lines = block[1].strip("\n").split("\n")
        for i, line in enumerate(lines):
            if i > 0:
                p.add_run().add_break(WD_BREAK.LINE)
            run = p.add_run(line)
            set_font(run, "Consolas", "宋体", 9.5)
    elif kind == "ul":
        for item in block[1]:
            p = doc.add_paragraph(style="List Bullet")
            no_indent(p)
            run = p.add_run(item)
            set_font(run, "Arial", "宋体", 11.5)
            p.paragraph_format.line_spacing = 1.4
    elif kind == "note":
        p = doc.add_paragraph(style="NoteText")
        shade_par(p, NOTE_FILL)
        r1 = p.add_run(block[1][0])
        set_font(r1, "Arial", "黑体", 11, bold=True)
        r2 = p.add_run(block[1][1])
        set_font(r2, "Arial", "宋体", 11)
    elif kind == "table":
        add_table(doc, block[1], block[2])
    elif kind == "pagebreak":
        doc.add_page_break()
    else:
        raise ValueError("unknown block " + kind)


def add_table(doc, headers, rows):
    table = doc.add_table(rows=1, cols=len(headers))
    table.style = "Table Grid"
    table.alignment = WD_TABLE_ALIGNMENT.CENTER
    hdr = table.rows[0].cells
    for i, h in enumerate(headers):
        cell = hdr[i]
        cell.vertical_alignment = WD_ALIGN_VERTICAL.CENTER
        tcpr = cell._tc.get_or_add_tcPr()
        shd = OxmlElement("w:shd")
        shd.set(qn("w:val"), "clear"); shd.set(qn("w:fill"), HEAD_FILL)
        tcpr.append(shd)
        par = cell.paragraphs[0]
        par.alignment = WD_ALIGN_PARAGRAPH.CENTER
        no_indent(par)
        run = par.add_run(h)
        set_font(run, "Arial", "黑体", 10.5, bold=True)
    for row in rows:
        cells = table.add_row().cells
        for i, val in enumerate(row):
            cells[i].vertical_alignment = WD_ALIGN_VERTICAL.CENTER
            par = cells[i].paragraphs[0]
            no_indent(par)
            par.paragraph_format.line_spacing = 1.25
            run = par.add_run(val)
            set_font(run, "Arial", "宋体", 10.5)
    # 段后留白
    doc.add_paragraph(style="Normal").paragraph_format.space_after = Pt(2)


def build():
    doc = Document()
    sec = doc.sections[0]
    sec.page_width, sec.page_height = Cm(21.0), Cm(29.7)
    sec.top_margin = sec.bottom_margin = Cm(2.5)
    sec.left_margin = sec.right_margin = Cm(2.5)
    configure_styles(doc)
    enable_update_fields(doc)

    # ---------- 封面 ----------
    for _ in range(3):
        doc.add_paragraph()
    t = doc.add_paragraph(); t.alignment = WD_ALIGN_PARAGRAPH.CENTER
    r = t.add_run("直播弹幕系统（Go 后端）")
    set_font(r, "Arial", "黑体", 22, bold=True)
    t2 = doc.add_paragraph(); t2.alignment = WD_ALIGN_PARAGRAPH.CENTER
    r = t2.add_run("校招 / 实习面试题库与参考答案")
    set_font(r, "Arial", "黑体", 18, bold=True)
    sub = doc.add_paragraph(); sub.alignment = WD_ALIGN_PARAGRAPH.CENTER
    r = sub.add_run("连接模型 · 并发路由 · Redis 跨节点 · Kafka 可靠落库")
    set_font(r, "Arial", "楷体", 12)
    for _ in range(2):
        doc.add_paragraph()

    # 简历原文块
    box = doc.add_paragraph(); no_indent(box); shade_par(box, CODE_FILL)
    r = box.add_run("简历原文\n"); set_font(r, "Arial", "黑体", 11.5, bold=True)
    desc = ("采用连接级 ReadPump/WritePump 双协程模型处理双向通信，通过房间级读写锁、"
            "客户端快照和广播 Worker Pool 完成多房间消息路由。设置有界广播任务队列与客户端发送队列，"
            "客户端队列拥塞时主动注销慢连接，避免单个客户端阻塞房间广播；使用 Redis Pub/Sub 将已持久化弹幕"
            "分发至各服务节点，并将在线人数变更按房间合并后定时广播；使用 kafka-go Async Writer 缓冲消息，"
            "由 Consumer Group 批量写入 MySQL，并通过消息唯一键保证落库幂等性。")
    r = box.add_run(desc); set_font(r, "Arial", "宋体", 11)
    box.paragraph_format.line_spacing = 1.5

    doc.add_paragraph()
    leg = doc.add_paragraph(); no_indent(leg)
    r = leg.add_run("标签说明："); set_font(r, "Arial", "黑体", 11, bold=True)
    r = leg.add_run("【做】实现细节  【因】选型权衡  【障】故障与边界  【放】开放题；"
                    "★ 越多区分度越高（最高三星）。")
    set_font(r, "Arial", "宋体", 11)
    use = doc.add_paragraph(); no_indent(use)
    r = use.add_run("使用建议："); set_font(r, "Arial", "黑体", 11, bold=True)
    r = use.add_run("先遮住参考答案自测口述，再对照订正；代码骨架建议亲手敲一遍，达到白板可写的程度。")
    set_font(r, "Arial", "宋体", 11)

    # ---------- 分节：封面 / 目录 / 正文 ----------
    s1 = doc.add_section(WD_SECTION.NEW_PAGE)
    s1.start_type = WD_SECTION.NEW_PAGE

    # ---------- 目录（标题不用 Heading，避免被 TOC 收录）----------
    p = doc.add_paragraph()
    p.alignment = WD_ALIGN_PARAGRAPH.CENTER
    p.paragraph_format.space_after = Pt(10)
    r = p.add_run("目  录")
    set_font(r, "Arial", "黑体", 16, bold=True)
    add_toc(doc)

    s2 = doc.add_section(WD_SECTION.NEW_PAGE)
    s2.start_type = WD_SECTION.NEW_PAGE

    # ---------- 正文 ----------
    for mod in ["c1", "c2", "c3", "c4", "c5", "c6", "c7"]:
        m = importlib.import_module(mod)
        for block in m.BLOCKS:
            render_block(doc, block)

    setup_sections_and_footers(doc)
    out = r"D:\GoProjects\live-stream\interview-doc\直播弹幕系统Go后端面试题库（含参考答案）.docx"
    doc.save(out)
    print("saved:", out)


def merge_section_breaks(doc):
    """把承载分节符的独立空段落合并进前一个段落，避免空段被挤到下一页形成空白页。"""
    body = doc.element.body
    for p_el in list(body.findall(qn("w:p"))):
        ppr = p_el.find(qn("w:pPr"))
        if ppr is None:
            continue
        sectpr = ppr.find(qn("w:sectPr"))
        if sectpr is None:
            continue
        prev = p_el.getprevious()
        if prev is not None and prev.tag == qn("w:p"):
            prev_ppr = prev.get_or_add_pPr()
            ppr.remove(sectpr)
            # pPr 中 sectPr 必须位于 rPr 之前
            rpr = prev_ppr.find(qn("w:rPr"))
            if rpr is not None:
                rpr.addprevious(sectpr)
            else:
                prev_ppr.append(sectpr)
            body.remove(p_el)


def setup_sections_and_footers(doc):
    """封面、目录不显示页码；正文节页脚居中页码并从 1 起编。"""
    merge_section_breaks(doc)
    # 强制分节类型为 nextPage
    for p_el in doc.element.body.findall(qn("w:p")):
        ppr = p_el.find(qn("w:pPr"))
        if ppr is None:
            continue
        sectpr = ppr.find(qn("w:sectPr"))
        if sectpr is None:
            continue
        t = sectpr.find(qn("w:type"))
        if t is None:
            t = OxmlElement("w:type")
            sectpr.insert(0, t)
        t.set(qn("w:val"), "nextPage")

    secs = doc.sections
    for idx, sec in enumerate(secs):
        sec.page_width, sec.page_height = Cm(21.0), Cm(29.7)
        sec.top_margin = sec.bottom_margin = Cm(2.5)
        sec.left_margin = sec.right_margin = Cm(2.5)
        footer = sec.footer
        footer.is_linked_to_previous = False
        par = footer.paragraphs[0]
        par.text = ""
        par.alignment = WD_ALIGN_PARAGRAPH.CENTER
        if idx == 2:  # 正文节
            run = add_field(par, "PAGE", "1")
            set_font(run, "Arial", "宋体", 10.5)
            sectpr = sec._sectPr
            pgnum = OxmlElement("w:pgNumType")
            pgnum.set(qn("w:start"), "1")
            sectpr.append(pgnum)


if __name__ == "__main__":
    build()
