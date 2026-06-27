// Knowledge Base frontend logic
// Handles page list, editor, search, graph, tags, and AI Q&A

var KB = (function() {
  'use strict';

  var API = '';
  var token = function() { return localStorage.getItem('token'); };
  var initialized = false;
  var currentPageTitle = null;
  var editMode = false;
  var currentPageBlocks = [];
  var currentPageID = 0;
  var isFavorite = false;
  var sidebarTab = 'pages'; // pages | favorites | recent

  function headers() {
    return { 'Authorization': 'Bearer ' + token(), 'Content-Type': 'application/json' };
  }

  async function apiGet(path) {
    var r = await fetch(API + '/api/kb' + path, { headers: { 'Authorization': 'Bearer ' + token() } });
    if (r.status === 401) { handleLogout(); return null; }
    return r.json();
  }

  async function apiPost(path, body) {
    var r = await fetch(API + '/api/kb' + path, {
      method: 'POST',
      headers: headers(),
      body: JSON.stringify(body || {})
    });
    if (r.status === 401) { handleLogout(); return null; }
    return r.json();
  }

  async function apiPut(path, body) {
    var r = await fetch(API + '/api/kb' + path, {
      method: 'PUT',
      headers: headers(),
      body: JSON.stringify(body || {})
    });
    if (r.status === 401) { handleLogout(); return null; }
    return r.json();
  }

  async function apiDelete(path) {
    var r = await fetch(API + '/api/kb' + path, {
      method: 'DELETE',
      headers: { 'Authorization': 'Bearer ' + token() }
    });
    if (r.status === 401) { handleLogout(); return null; }
    return r.json();
  }

  function esc(s) {
    if (s == null) return '';
    return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
  }

  // Render markdown (simple: headings, bold, links, code, lists)
  function renderMd(text) {
    if (!text) return '';
    var html = esc(text);
    // Code blocks
    html = html.replace(/```(\w*)\n([\s\S]*?)```/g, function(m, lang, code) {
      return '<pre><code>' + code + '</code></pre>';
    });
    // Headings
    html = html.replace(/^### (.+)$/gm, '<h3>$1</h3>');
    html = html.replace(/^## (.+)$/gm, '<h2>$1</h2>');
    html = html.replace(/^# (.+)$/gm, '<h1>$1</h1>');
    // Bidirectional links [[Page Name]]
    html = html.replace(/\[\[([^\]]+)\]\]/g, function(m, name) {
      return '<a class="kb-wikilink" href="#" onclick="KB.openPage(\'' + esc(name).replace(/'/g,"\\'") + '\');return false;">' + esc(name) + '</a>';
    });
    // Tags #tag
    html = html.replace(/(^|\s)#(\w[\w-]*)/g, function(m, pre, tag) {
      return pre + '<span class="kb-tag">#' + esc(tag) + '</span>';
    });
    // Bold
    html = html.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
    // Inline code
    html = html.replace(/`([^`]+)`/g, '<code>$1</code>');
    // Lists
    html = html.replace(/^- (.+)$/gm, '<li>$1</li>');
    html = html.replace(/(<li>[\s\S]*?<\/li>)/g, '<ul>$1</ul>');
    // Paragraphs
    html = html.replace(/\n\n/g, '</p><p>');
    html = '<p>' + html + '</p>';
    html = html.replace(/<p><\/p>/g, '');
    html = html.replace(/<p>(<h[1-3]>)/g, '$1');
    html = html.replace(/(<\/h[1-3]>)<\/p>/g, '$1');
    html = html.replace(/<p>(<pre>)/g, '$1');
    html = html.replace(/(<\/pre>)<\/p>/g, '$1');
    html = html.replace(/<p>(<ul>)/g, '$1');
    html = html.replace(/(<\/ul>)<\/p>/g, '$1');
    return html;
  }

  // --- Init ---
  function init() {
    if (initialized) { loadPages(); return; }
    initialized = true;
    loadPages();
  }

  // --- Page List ---
  async function loadPages() {
    var data = await apiGet('/pages');
    if (!data || !data.pages) {
      document.getElementById('kbPageList').innerHTML = '<div class="empty-state"><p>Failed to load pages.</p></div>';
      return;
    }
    renderPageList(data.pages);
  }

  function renderPageList(pages) {
    var el = document.getElementById('kbPageList');
    if (!pages.length) {
      el.innerHTML = '<div class="empty-state"><p>No pages yet. Click "+ New Page" to create one.</p></div>';
      return;
    }
    el.innerHTML = pages.map(function(p) {
      var tags = (p.tags || []).map(function(t) { return '<span class="kb-tag-sm">#' + esc(t) + '</span>'; }).join('');
      var active = (p.title === currentPageTitle) ? ' active' : '';
      return '<div class="kb-page-item' + active + '" onclick="KB.openPage(\'' + esc(p.title).replace(/'/g,"\\'") + '\')">' +
        '<div class="kb-page-title">' + esc(p.title) + '</div>' +
        (tags ? '<div class="kb-page-tags">' + tags + '</div>' : '') +
        '</div>';
    }).join('');
  }

  // --- Open / View Page ---
  async function openPage(title) {
    currentPageTitle = title;
    editMode = false;
    var data = await apiGet('/pages/' + encodeURIComponent(title));
    if (!data || !data.page) {
      document.getElementById('kbContent').innerHTML = '<div class="empty-state"><p>Page not found: ' + esc(title) + '</p></div>';
      return;
    }
    // 检查收藏状态
    var favs = await apiGet('/favorites');
    if (favs && favs.pages) {
      isFavorite = favs.pages.some(function(p) { return p.title === title; });
    }
    renderPage(data);
    loadPages(); // refresh active state
  }

  function renderPage(data) {
    var page = data.page;
    var blocks = data.blocks || [];
    var backlinks = data.backlinks || [];
    var outlinks = data.outlinks || [];

    currentPageBlocks = blocks;
    currentPageID = page.id;

    var content = blocks.map(function(b) {
      var prefix = '';
      for (var i = 0; i < (b.level || 0); i++) prefix += '  ';
      return prefix + (b.content || '');
    }).join('\n');

    var tagsHtml = (page.tags || []).map(function(t) {
      return '<span class="kb-tag">#' + esc(t) + '</span>';
    }).join(' ');

    var backlinksHtml = backlinks.length ? backlinks.map(function(bl) {
      return '<div class="kb-backlink-item">' +
        '<a class="kb-wikilink" href="#" onclick="KB.openPage(\'' + esc(bl.page_title).replace(/'/g,"\\'") + '\');return false;">' + esc(bl.page_title) + '</a>' +
        '<span class="kb-backlink-snippet">' + esc((bl.block_content || '').slice(0, 100)) + '</span>' +
        '</div>';
    }).join('') : '<span class="kb-muted">No backlinks</span>';

    var favBtn = isFavorite ? '★' : '☆';

    var html = '<div class="kb-page-view">' +
      '<div class="kb-page-header">' +
        '<div class="kb-page-title-row">' +
          '<h1>' + esc(page.title) + '</h1>' +
          '<button class="kb-fav-btn" onclick="KB.toggleFavorite(\'' + esc(page.title).replace(/'/g,"\\'") + '\')" title="Favorite">' + favBtn + '</button>' +
        '</div>' +
        '<div class="kb-page-actions">' +
          '<button class="btn-sm" onclick="KB.toggleEdit()">Edit</button>' +
          '<button class="btn-sm" onclick="KB.suggestSummary(\'' + esc(page.title).replace(/'/g,"\\'") + '\')">AI Summary</button>' +
          '<button class="btn-sm" onclick="KB.suggestLinks()">Suggest Links</button>' +
          '<button class="btn-sm" onclick="KB.showUnlinked(\'' + esc(page.title).replace(/'/g,"\\'") + '\')">Unlinked Refs</button>' +
          '<button class="btn-sm" onclick="KB.exportPage(\'' + esc(page.title).replace(/'/g,"\\'") + '\',\'html\')">Export HTML</button>' +
          '<button class="btn-sm danger" onclick="KB.deletePage(\'' + esc(page.title).replace(/'/g,"\\'") + '\')">Delete</button>' +
        '</div>' +
      '</div>' +
      (tagsHtml ? '<div class="kb-page-tags-row">' + tagsHtml + '</div>' : '') +
      '<div class="kb-page-body" id="kbPageBody">' + renderBlocks(blocks) + '</div>' +
      '<div class="kb-backlinks-section">' +
        '<h3>Backlinks (' + backlinks.length + ')</h3>' +
        '<div class="kb-backlinks-list">' + backlinksHtml + '</div>' +
      '</div>' +
    '</div>';

    document.getElementById('kbContent').innerHTML = html;
    document.getElementById('kbContent').dataset.rawContent = content;
    // 记录最近访问
    apiPost('/recent', {}).catch(function(){});
    // 加载属性
    loadProperties(page.title);
    // 启用块拖拽
    enableBlockDrag();
  }

  // 渲染块列表（带拖拽手柄）
  function renderBlocks(blocks) {
    if (!blocks || !blocks.length) return '<p class="kb-muted">Empty page</p>';
    return blocks.map(function(b, i) {
      var indent = (b.level || 0) * 20;
      var typeClass = 'kb-block kb-block-' + (b.block_type || 'paragraph');
      return '<div class="' + typeClass + '" data-block-id="' + esc(b.id) + '" data-order="' + i + '" style="margin-left:' + indent + 'px">' +
        '<span class="kb-block-handle" draggable="true" title="Drag to reorder">⋮⋮</span>' +
        '<div class="kb-block-content">' + renderMd(b.content || '') + '</div>' +
        '</div>';
    }).join('');
  }

  // 启用块拖拽排序
  function enableBlockDrag() {
    var body = document.getElementById('kbPageBody');
    if (!body) return;
    var dragSrc = null;
    body.querySelectorAll('.kb-block').forEach(function(block) {
      var handle = block.querySelector('.kb-block-handle');
      handle.addEventListener('dragstart', function(e) {
        dragSrc = block;
        e.dataTransfer.effectAllowed = 'move';
        block.classList.add('kb-dragging');
      });
      handle.addEventListener('dragend', function() {
        block.classList.remove('kb-dragging');
      });
      block.addEventListener('dragover', function(e) {
        e.preventDefault();
        if (dragSrc && dragSrc !== block) {
          block.classList.add('kb-drag-over');
        }
      });
      block.addEventListener('dragleave', function() {
        block.classList.remove('kb-drag-over');
      });
      block.addEventListener('drop', function(e) {
        e.preventDefault();
        block.classList.remove('kb-drag-over');
        if (!dragSrc || dragSrc === block) return;
        var blockID = dragSrc.dataset.blockId;
        var afterBlockID = block.dataset.blockId;
        apiPost('/blocks/reorder', { block_id: blockID, after_block_id: afterBlockID, page_id: currentPageID }).then(function() {
          openPage(currentPageTitle);
        });
      });
    });
  }

  // 加载并渲染属性面板
  async function loadProperties(title) {
    var data = await apiGet('/pages/' + encodeURIComponent(title) + '/properties');
    if (!data || !data.properties) return;
    var html = '<div class="kb-props-panel" id="kbPropsPanel">' +
      '<h4>Properties</h4>';
    if (data.properties.length === 0) {
      html += '<p class="kb-muted">No properties. Add via frontmatter or the button below.</p>';
    } else {
      html += data.properties.map(function(p) {
        return '<div class="kb-prop-row">' +
          '<span class="kb-prop-name">' + esc(p.name) + '</span>' +
          '<span class="kb-prop-type">' + esc(p.type) + '</span>' +
          '<span class="kb-prop-value">' + esc(JSON.stringify(p.value)) + '</span>' +
          '<button class="kb-prop-del" onclick="KB.deleteProperty(\'' + esc(title).replace(/'/g,"\\'") + '\',\'' + esc(p.name) + '\')">x</button>' +
          '</div>';
      }).join('');
    }
    html += '<div class="kb-prop-add">' +
      '<input type="text" id="kbPropName" placeholder="name">' +
      '<input type="text" id="kbPropValue" placeholder="value">' +
      '<select id="kbPropType"><option value="string">string</option><option value="number">number</option><option value="boolean">boolean</option><option value="tags">tags</option></select>' +
      '<button class="btn-sm" onclick="KB.addProperty(\'' + esc(title).replace(/'/g,"\\'") + '\')">Add</button>' +
      '</div></div>';
    var existing = document.getElementById('kbPropsPanel');
    if (existing) existing.remove();
    var header = document.querySelector('.kb-page-header');
    if (header) header.insertAdjacentHTML('afterend', html);
  }

  // --- Edit Mode (Rich Block Editor) ---
  function toggleEdit() {
    if (!currentPageTitle) return;
    editMode = !editMode;
    if (editMode) {
      var raw = document.getElementById('kbContent').dataset.rawContent || '';
      document.getElementById('kbContent').innerHTML =
        '<div class="kb-editor">' +
          '<input type="text" id="kbEditTitle" class="kb-edit-title" value="' + esc(currentPageTitle) + '">' +
          '<div class="kb-editor-toolbar">' +
            '<button type="button" class="kb-tool-btn" onclick="KBEditor.toggleBold()" title="Bold"><b>B</b></button>' +
            '<button type="button" class="kb-tool-btn" onclick="KBEditor.toggleItalic()" title="Italic"><i>I</i></button>' +
            '<button type="button" class="kb-tool-btn" onclick="KBEditor.toggleStrike()" title="Strikethrough"><s>S</s></button>' +
            '<button type="button" class="kb-tool-btn" onclick="KBEditor.toggleCode()" title="Code">&lt;/&gt;</button>' +
            '<span class="kb-tool-sep"></span>' +
            '<button type="button" class="kb-tool-btn" onclick="KBEditor.setBlockType(null,\'h1\')" title="Heading 1">H1</button>' +
            '<button type="button" class="kb-tool-btn" onclick="KBEditor.setBlockType(null,\'h2\')" title="Heading 2">H2</button>' +
            '<button type="button" class="kb-tool-btn" onclick="KBEditor.setBlockType(null,\'h3\')" title="Heading 3">H3</button>' +
            '<button type="button" class="kb-tool-btn" onclick="KBEditor.setBlockType(null,\'p\')" title="Paragraph">P</button>' +
            '<span class="kb-tool-sep"></span>' +
            '<button type="button" class="kb-tool-btn" onclick="KBEditor.setBlockType(null,\'ul\')" title="Bullet list">• List</button>' +
            '<button type="button" class="kb-tool-btn" onclick="KBEditor.setBlockType(null,\'ol\')" title="Numbered list">1. List</button>' +
            '<button type="button" class="kb-tool-btn" onclick="KBEditor.setBlockType(null,\'todo\')" title="Todo">TODO</button>' +
            '<button type="button" class="kb-tool-btn" onclick="KBEditor.setBlockType(null,\'quote\')" title="Quote">Quote</button>' +
            '<button type="button" class="kb-tool-btn" onclick="KBEditor.setBlockType(null,\'code\')" title="Code block">{ }</button>' +
            '<button type="button" class="kb-tool-btn" onclick="KBEditor.setBlockType(null,\'divider\')" title="Divider">―</button>' +
            '<span class="kb-tool-sep"></span>' +
            '<button type="button" class="kb-tool-btn" onclick="KBEditor.insertWikiLink()" title="Page link">[[Link]]</button>' +
            '<button type="button" class="kb-tool-btn" onclick="KBEditor.insertTag()" title="Tag">#Tag</button>' +
            '<span class="kb-tool-sep"></span>' +
            '<button type="button" class="kb-tool-btn" onclick="KB.aiContinue()" title="AI continue writing">✨ AI Continue</button>' +
          '</div>' +
          '<div id="kbEditContent" class="kb-edit-content"></div>' +
          '<div class="kb-editor-actions">' +
            '<button class="btn-sm primary" onclick="KB.savePage()">Save</button>' +
            '<button class="btn-sm" onclick="KB.toggleEdit()">Cancel</button>' +
            '<button class="btn-sm" onclick="KB.suggestTagsForEditor()">Suggest Tags</button>' +
          '</div>' +
        '</div>';
      // 挂载块级编辑器
      var container = document.getElementById('kbEditContent');
      KBEditor.mount(container, raw);
      KBEditor.setupSlashMenu(container);
      // 聚焦第一个块
      var firstBlock = container.querySelector('.kb-block');
      if (firstBlock) firstBlock.focus();
    } else {
      openPage(currentPageTitle);
    }
  }

  // 编辑器：包裹文本
  function insertMd(before, after, kind) {
    var ta = document.getElementById('kbEditContent');
    if (!ta) return;
    var start = ta.selectionStart, end = ta.selectionEnd;
    var sel = ta.value.substring(start, end);
    var insert = before + (sel || 'text') + after;
    ta.value = ta.value.substring(0, start) + insert + ta.value.substring(end);
    var cursorPos = start + before.length;
    ta.selectionStart = ta.selectionEnd = cursorPos + (sel || 'text').length;
    ta.focus();
  }

  // 编辑器：行首插入前缀
  function insertLinePrefix(prefix) {
    var ta = document.getElementById('kbEditContent');
    if (!ta) return;
    var start = ta.selectionStart;
    var lineStart = ta.value.lastIndexOf('\n', start - 1) + 1;
    ta.value = ta.value.substring(0, lineStart) + prefix + ta.value.substring(lineStart);
    ta.selectionStart = ta.selectionEnd = start + prefix.length;
    ta.focus();
  }

  // 编辑器：斜杠命令
  var slashCommands = [
    { key: 'h2', label: 'Heading 2', insert: '## ' },
    { key: 'h3', label: 'Heading 3', insert: '### ' },
    { key: 'bullet', label: 'Bullet List', insert: '- ' },
    { key: 'number', label: 'Numbered List', insert: '1. ' },
    { key: 'todo', label: 'Todo', insert: '- [ ] ' },
    { key: 'quote', label: 'Quote', insert: '> ' },
    { key: 'code', label: 'Code Block', insert: '\n```\n\n```\n' },
    { key: 'link', label: 'Page Link', insert: '[[]]' },
    { key: 'tag', label: 'Tag', insert: '#' },
    { key: 'divider', label: 'Divider', insert: '\n---\n' },
    { key: 'ai', label: '✨ AI Continue', insert: '__AI_CONTINUE__' }
  ];

  function handleEditorKey(e) {
    var ta = e.target;
    // 斜杠命令触发
    if (e.key === '/' && ta.selectionStart === ta.selectionEnd) {
      var pos = ta.selectionStart;
      var before = ta.value.substring(0, pos);
      // 行首或空格后
      if (pos === 0 || before[pos-1] === '\n' || before[pos-1] === ' ') {
        setTimeout(function() { showSlashMenu(ta); }, 0);
      }
    }
    // Tab 缩进
    if (e.key === 'Tab') {
      e.preventDefault();
      var s = ta.selectionStart, en = ta.selectionEnd;
      ta.value = ta.value.substring(0, s) + '  ' + ta.value.substring(en);
      ta.selectionStart = ta.selectionEnd = s + 2;
    }
  }

  function handleEditorInput(e) {
    var ta = e.target;
    var menu = document.getElementById('kbSlashMenu');
    if (!menu) return;
    if (menu.style.display === 'none') return;
    // 更新过滤
    showSlashMenu(ta);
  }

  function showSlashMenu(ta) {
    var menu = document.getElementById('kbSlashMenu');
    if (!menu) return;
    var pos = ta.selectionStart;
    var before = ta.value.substring(0, pos);
    var slashIdx = before.lastIndexOf('/');
    if (slashIdx < 0 || slashIdx !== before.length - 1) {
      // 检查是否在 / 后输入了字符
      var m = before.match(/\/(\w*)$/);
      if (!m) { menu.style.display = 'none'; return; }
      slashIdx = before.length - m[0].length;
      var filter = m[1].toLowerCase();
      var matched = slashCommands.filter(function(c) { return c.label.toLowerCase().indexOf(filter) >= 0 || c.key.indexOf(filter) >= 0; });
      if (!matched.length) { menu.style.display = 'none'; return; }
      renderSlashMenu(menu, matched, ta, slashIdx);
    } else {
      renderSlashMenu(menu, slashCommands, ta, slashIdx);
    }
  }

  function renderSlashMenu(menu, cmds, ta, slashIdx) {
    menu.innerHTML = cmds.map(function(c, i) {
      return '<div class="kb-slash-item" data-idx="' + i + '" onclick="KB.applySlash(' + i + ',\'' + c.key + '\')">' + esc(c.label) + '</div>';
    }).join('');
    menu.style.display = 'block';
    menu.dataset.slashIdx = slashIdx;
    // 定位（简单放在编辑器下方）
    var rect = ta.getBoundingClientRect();
    menu.style.left = rect.left + 'px';
    menu.style.top = (rect.top + 30) + 'px';
    menu.dataset.cmds = JSON.stringify(cmds.map(function(c){return c.insert;}));
  }

  function applySlash(idx, key) {
    var ta = document.getElementById('kbEditContent');
    var menu = document.getElementById('kbSlashMenu');
    if (!ta || !menu) return;
    var slashIdx = parseInt(menu.dataset.slashIdx);
    var cmds = JSON.parse(menu.dataset.cmds);
    var insert = cmds[idx];
    if (key === 'ai') {
      // AI 续写
      menu.style.display = 'none';
      ta.value = ta.value.substring(0, slashIdx) + ta.value.substring(slashIdx + 1);
      aiContinue();
      return;
    }
    ta.value = ta.value.substring(0, slashIdx) + insert + ta.value.substring(slashIdx + 1);
    var newPos = slashIdx + insert.length;
    ta.selectionStart = ta.selectionEnd = newPos;
    menu.style.display = 'none';
    ta.focus();
  }

  // AI 续写
  async function aiContinue() {
    var container = document.getElementById('kbEditContent');
    if (!container) return;
    var content = KBEditor.getMarkdown(container);
    var title = document.getElementById('kbEditTitle').value || currentPageTitle;
    var btn = event && event.target;
    var original = btn ? btn.textContent : '';
    if (btn) { btn.disabled = true; btn.textContent = '✨ Thinking...'; }
    try {
      var data = await apiPost('/suggest/continue', { title: title, content: content });
      if (data && data.continuation) {
        // 将续写内容解析为块并追加到编辑器
        var editor = container.querySelector('.kb-block-editor');
        if (editor) {
          var newBlocks = KBEditor.parseMarkdown(data.continuation);
          // 用临时容器生成 DOM
          var tmp = document.createElement('div');
          KBEditor.mount(tmp, data.continuation);
          while (tmp.firstChild) {
            editor.appendChild(tmp.firstChild);
          }
        }
      } else if (data && data.error) {
        alert(data.error);
      }
    } finally {
      if (btn) { btn.disabled = false; btn.textContent = original; }
    }
  }

  async function savePage() {
    var title = document.getElementById('kbEditTitle').value.trim();
    var container = document.getElementById('kbEditContent');
    var content = KBEditor.getMarkdown(container);
    if (!title) { alert('Title is required'); return; }

    var data;
    if (title === currentPageTitle) {
      data = await apiPut('/pages/' + encodeURIComponent(currentPageTitle), { content: content });
    } else {
      data = await apiPost('/pages', { title: title, content: content });
    }

    if (data && data.error) {
      alert(data.error);
      return;
    }

    currentPageTitle = title;
    editMode = false;
    openPage(title);
  }

  // --- New Page ---
  function newPage() {
    currentPageTitle = null;
    editMode = true;
    document.getElementById('kbPageList').querySelectorAll('.kb-page-item').forEach(function(el) {
      el.classList.remove('active');
    });
    document.getElementById('kbContent').innerHTML =
      '<div class="kb-editor">' +
        '<input type="text" id="kbEditTitle" class="kb-edit-title" placeholder="Page title...">' +
        '<div class="kb-editor-toolbar">' +
          '<button type="button" class="kb-tool-btn" onclick="KBEditor.toggleBold()" title="Bold"><b>B</b></button>' +
          '<button type="button" class="kb-tool-btn" onclick="KBEditor.toggleItalic()" title="Italic"><i>I</i></button>' +
          '<button type="button" class="kb-tool-btn" onclick="KBEditor.toggleStrike()" title="Strikethrough"><s>S</s></button>' +
          '<button type="button" class="kb-tool-btn" onclick="KBEditor.toggleCode()" title="Code">&lt;/&gt;</button>' +
          '<span class="kb-tool-sep"></span>' +
          '<button type="button" class="kb-tool-btn" onclick="KBEditor.setBlockType(null,\'h1\')" title="Heading 1">H1</button>' +
          '<button type="button" class="kb-tool-btn" onclick="KBEditor.setBlockType(null,\'h2\')" title="Heading 2">H2</button>' +
          '<button type="button" class="kb-tool-btn" onclick="KBEditor.setBlockType(null,\'h3\')" title="Heading 3">H3</button>' +
          '<button type="button" class="kb-tool-btn" onclick="KBEditor.setBlockType(null,\'ul\')" title="Bullet list">• List</button>' +
          '<button type="button" class="kb-tool-btn" onclick="KBEditor.setBlockType(null,\'ol\')" title="Numbered list">1. List</button>' +
          '<button type="button" class="kb-tool-btn" onclick="KBEditor.setBlockType(null,\'todo\')" title="Todo">TODO</button>' +
          '<button type="button" class="kb-tool-btn" onclick="KBEditor.setBlockType(null,\'quote\')" title="Quote">Quote</button>' +
          '<button type="button" class="kb-tool-btn" onclick="KBEditor.setBlockType(null,\'code\')" title="Code block">{ }</button>' +
          '<span class="kb-tool-sep"></span>' +
          '<button type="button" class="kb-tool-btn" onclick="KBEditor.insertWikiLink()" title="Page link">[[Link]]</button>' +
          '<button type="button" class="kb-tool-btn" onclick="KBEditor.insertTag()" title="Tag">#Tag</button>' +
        '</div>' +
        '<div id="kbEditContent" class="kb-edit-content"></div>' +
        '<div class="kb-editor-actions">' +
          '<button class="btn-sm primary" onclick="KB.createPage()">Create</button>' +
          '<button class="btn-sm" onclick="KB.cancelNew()">Cancel</button>' +
        '</div>' +
      '</div>';
    // 挂载块级编辑器
    var container = document.getElementById('kbEditContent');
    KBEditor.mount(container, '# New Page\n\nStart writing here...\n');
    KBEditor.setupSlashMenu(container);
    document.getElementById('kbEditTitle').focus();
  }

  function cancelNew() {
    editMode = false;
    currentPageTitle = null;
    document.getElementById('kbContent').innerHTML =
      '<div class="empty-state"><p>Select a page or create a new one.</p></div>';
  }

  async function createPage() {
    var title = document.getElementById('kbEditTitle').value.trim();
    var container = document.getElementById('kbEditContent');
    var content = KBEditor.getMarkdown(container);
    if (!title) { alert('Title is required'); return; }

    var data = await apiPost('/pages', { title: title, content: content });
    if (data && data.error) {
      alert(data.error);
      return;
    }

    currentPageTitle = title;
    editMode = false;
    loadPages();
    openPage(title);
  }

  // --- Delete Page ---
  async function deletePage(title) {
    if (!confirm('Delete page "' + title + '"? This cannot be undone.')) return;
    await apiDelete('/pages/' + encodeURIComponent(title));
    if (currentPageTitle === title) {
      currentPageTitle = null;
      document.getElementById('kbContent').innerHTML =
        '<div class="empty-state"><p>Select a page or create a new one.</p></div>';
    }
    loadPages();
  }

  // --- Search ---
  function handleSearchKey(e) {
    if (e.key === 'Enter') { e.preventDefault(); search(); }
  }

  async function search() {
    var q = document.getElementById('kbSearchInput').value.trim();
    if (!q) { loadPages(); return; }

    var data = await apiGet('/search?q=' + encodeURIComponent(q));
    if (!data || !data.results) {
      document.getElementById('kbPageList').innerHTML = '<div class="empty-state"><p>Search failed.</p></div>';
      return;
    }

    var results = data.results;
    var el = document.getElementById('kbPageList');
    if (!results.length) {
      el.innerHTML = '<div class="empty-state"><p>No results for "' + esc(q) + '"</p></div>';
      return;
    }
    el.innerHTML = '<div class="kb-search-header">Results (' + results.length + ')</div>' +
      results.map(function(r) {
        return '<div class="kb-page-item" onclick="KB.openPage(\'' + esc(r.title).replace(/'/g,"\\'") + '\')">' +
          '<div class="kb-page-title">' + esc(r.title) + '</div>' +
          '<div class="kb-search-snippet">' + esc(r.snippet || '') + '</div>' +
          '</div>';
      }).join('');
  }

  // --- Tags View ---
  async function showTags() {
    var data = await apiGet('/tags');
    if (!data || !data.tags) {
      document.getElementById('kbContent').innerHTML = '<div class="empty-state"><p>Failed to load tags.</p></div>';
      return;
    }

    var tags = data.tags;
    var html = '<div class="kb-tags-view"><h2>Tags</h2>';
    if (!tags.length) {
      html += '<p class="kb-muted">No tags yet.</p>';
    } else {
      html += '<div class="kb-tag-cloud">';
      html += tags.map(function(t) {
        return '<span class="kb-tag-item" onclick="KB.showPagesByTag(\'' + esc(t.name).replace(/'/g,"\\'") + '\')">#' + esc(t.name) + ' <span class="kb-tag-count">' + t.count + '</span></span>';
      }).join('');
      html += '</div>';
    }
    html += '</div>';
    document.getElementById('kbContent').innerHTML = html;
  }

  async function showPagesByTag(tag) {
    var data = await apiGet('/tags/' + encodeURIComponent(tag) + '/pages');
    if (!data || !data.pages) return;

    var html = '<div class="kb-tag-pages"><h2>Pages tagged #' + esc(tag) + '</h2>';
    html += '<div class="kb-page-grid">';
    html += data.pages.map(function(p) {
      return '<div class="kb-page-card" onclick="KB.openPage(\'' + esc(p.title).replace(/'/g,"\\'") + '\')">' +
        '<div class="kb-page-title">' + esc(p.title) + '</div>' +
        '</div>';
    }).join('');
    html += '</div></div>';
    document.getElementById('kbContent').innerHTML = html;
  }

  // --- Graph View ---
  async function showGraph() {
    var data = await apiGet('/graph');
    if (!data || !data.nodes) {
      document.getElementById('kbContent').innerHTML = '<div class="empty-state"><p>Failed to load graph.</p></div>';
      return;
    }

    var nodes = data.nodes || [];
    var edges = data.edges || [];

    var html = '<div class="kb-graph-view">' +
      '<h2>Knowledge Graph</h2>' +
      '<p class="kb-muted">' + nodes.length + ' nodes, ' + edges.length + ' links · Drag nodes, scroll to zoom, click to open</p>';

    if (nodes.length === 0) {
      html += '<p>No pages to display.</p>';
    } else {
      // Simple force-directed layout using SVG
      html += '<div class="kb-graph-container" id="kbGraphContainer"></div>';
      html += '<div class="kb-graph-legend">' +
        '<h3>Top Connected Pages</h3><ul>';
      var sorted = nodes.slice().sort(function(a,b) { return (b.size||0) - (a.size||0); }).slice(0, 15);
      html += sorted.map(function(n) {
        return '<li><a class="kb-wikilink" href="#" onclick="KB.openPage(\'' + esc(n.label).replace(/'/g,"\\'") + '\');return false;">' + esc(n.label) + '</a> (' + (n.size || 0) + ')</li>';
      }).join('');
      html += '</ul></div>';
    }
    html += '</div>';

    document.getElementById('kbContent').innerHTML = html;

    if (nodes.length > 0) {
      drawGraph(nodes, edges);
    }
  }

  function drawGraph(nodes, edges) {
    var container = document.getElementById('kbGraphContainer');
    if (!container) return;

    var w = container.clientWidth || 600;
    var h = 420;

    // 使用力导向布局渲染
    KBGraph.render(container, nodes, edges, {
      width: w,
      height: h,
      chargeStrength: -150,
      linkDistance: 90,
      onNodeClick: function (n) {
        if (n.label) KB.openPage(n.label);
      }
    });
  }

  // --- AI Q&A ---
  function showQA() {
    document.getElementById('kbContent').innerHTML =
      '<div class="kb-qa-view">' +
        '<h2>Ask the Knowledge Base</h2>' +
        '<p class="kb-muted">Ask a question and get an answer based on your knowledge base.</p>' +
        '<div class="kb-qa-input-row">' +
          '<input type="text" id="kbQAInput" class="kb-qa-input" placeholder="Ask a question..." onkeydown="if(event.key===\'Enter\')KB.askQA()">' +
          '<select id="kbQAMode" class="kb-qa-mode">' +
            '<option value="hybrid">Hybrid</option>' +
            '<option value="keyword">Keyword</option>' +
            '<option value="semantic">Semantic</option>' +
          '</select>' +
          '<button class="btn-sm primary" onclick="KB.askQA()">Ask</button>' +
        '</div>' +
        '<div class="kb-qa-answer" id="kbQAAnswer"></div>' +
        '<div class="kb-qa-sources" id="kbQASources"></div>' +
      '</div>';
    document.getElementById('kbQAInput').focus();
  }

  async function askQA() {
    var q = document.getElementById('kbQAInput').value.trim();
    if (!q) return;
    var mode = document.getElementById('kbQAMode').value;

    var answerEl = document.getElementById('kbQAAnswer');
    var sourcesEl = document.getElementById('kbQASources');
    answerEl.innerHTML = '<div class="kb-qa-thinking">Thinking...</div>';
    sourcesEl.innerHTML = '';

    try {
      var resp = await fetch(API + '/api/kb/qa', {
        method: 'POST',
        headers: headers(),
        body: JSON.stringify({ question: q, mode: mode })
      });

      var reader = resp.body.getReader();
      var decoder = new TextDecoder();
      var fullText = '';
      var sources = null;

      answerEl.innerHTML = '<div class="kb-qa-streaming"></div>';
      var streamEl = answerEl.querySelector('.kb-qa-streaming');

      while (true) {
        var chunk = await reader.read();
        if (chunk.done) break;
        var text = decoder.decode(chunk.value, { stream: true });
        var lines = text.split('\n');
        for (var i = 0; i < lines.length; i++) {
          var line = lines[i].trim();
          if (!line.startsWith('data: ')) continue;
          var jsonStr = line.slice(6);
          try {
            var data = JSON.parse(jsonStr);
            if (data.text) {
              fullText += data.text;
              streamEl.innerHTML = renderMd(fullText);
            }
            if (data.sources && data.sources.length) {
              sources = data.sources;
            }
            if (data.error) {
              streamEl.innerHTML = '<div class="kb-error">' + esc(data.error) + '</div>';
            }
          } catch(e) {}
        }
      }

      if (sources) {
        sourcesEl.innerHTML = '<h3>Sources</h3><div class="kb-qa-source-list">' +
          sources.map(function(s) {
            return '<div class="kb-qa-source-item">' +
              '<a class="kb-wikilink" href="#" onclick="KB.openPage(\'' + esc(s.page_title).replace(/'/g,"\\'") + '\');return false;">' + esc(s.page_title) + '</a>' +
              '<span class="kb-qa-source-snippet">' + esc((s.snippet || '').slice(0, 120)) + '</span>' +
              '</div>';
          }).join('') + '</div>';
      }
    } catch(e) {
      answerEl.innerHTML = '<div class="kb-error">Failed to get answer: ' + esc(e.message) + '</div>';
    }
  }

  // --- AI Suggestions ---
  async function suggestSummary(title) {
    var el = document.querySelector('.kb-page-body');
    if (el) el.innerHTML = '<div class="kb-qa-thinking">Generating summary...</div>';
    var data = await apiGet('/suggest/summary/' + encodeURIComponent(title));
    if (data && data.summary) {
      var summaryHtml = '<div class="kb-ai-summary"><div class="kb-ai-badge">AI Summary</div>' + renderMd(data.summary) + '</div>';
      var body = document.querySelector('.kb-page-body');
      if (body) {
        body.insertAdjacentHTML('beforebegin', summaryHtml);
      }
    } else if (data && data.error) {
      alert(data.error);
    }
  }

  async function suggestLinks() {
    var content = document.getElementById('kbContent').dataset.rawContent || '';
    if (!content) return;
    var data = await apiPost('/suggest/links', { content: content });
    if (!data || !data.suggestions) {
      alert('No suggestions available');
      return;
    }
    var html = '<div class="kb-suggest-overlay" id="kbSuggestOverlay">' +
      '<div class="kb-suggest-modal">' +
        '<div class="kb-suggest-header"><h3>Suggested Links</h3><button onclick="document.getElementById(\'kbSuggestOverlay\').remove()">✕</button></div>' +
        '<div class="kb-suggest-body">';
    if (!data.suggestions.length) {
      html += '<p class="kb-muted">No link suggestions.</p>';
    } else {
      html += data.suggestions.map(function(s) {
        return '<div class="kb-suggest-item">' +
          '<div class="kb-suggest-text">"' + esc(s.text) + '"</div>' +
          '<div class="kb-suggest-target">→ <a class="kb-wikilink" href="#" onclick="KB.openPage(\'' + esc(s.page_title).replace(/'/g,"\\'") + '\');return false;">' + esc(s.page_title) + '</a>' + (s.exists ? '' : ' <span class="kb-badge-new">new</span>') + '</div>' +
          '<div class="kb-suggest-reason">' + esc(s.reason) + '</div>' +
        '</div>';
      }).join('');
    }
    html += '</div></div></div>';
    document.body.insertAdjacentHTML('beforeend', html);
  }

  async function suggestTagsForEditor() {
    var title = document.getElementById('kbEditTitle').value.trim();
    var container = document.getElementById('kbEditContent');
    var content = KBEditor.getMarkdown(container);
    if (!title) { alert('Title required'); return; }
    var data = await apiPost('/suggest/tags', { title: title, content: content });
    if (!data || !data.tags) { alert('No tag suggestions'); return; }
    var tagStr = data.tags.map(function(t) { return '#' + t; }).join(' ');
    // 在编辑器末尾追加标签块
    var editor = container.querySelector('.kb-block-editor');
    if (editor) {
      var tagBlock = KBEditor.parseMarkdown(tagStr)[0];
      if (tagBlock) {
        editor.appendChild(KBEditor.mount(document.createElement('div'), tagStr).firstChild);
      }
    }
  }

  // --- Sync ---
  async function sync() {
    var data = await apiPost('/sync', {});
    if (data && data.message) {
      alert(data.message);
      loadPages();
    }
  }

  // --- 收藏夹 ---
  async function toggleFavorite(title) {
    if (isFavorite) {
      await apiDelete('/favorites/' + encodeURIComponent(title));
      isFavorite = false;
    } else {
      await apiPost('/favorites/' + encodeURIComponent(title), {});
      isFavorite = true;
    }
    openPage(title);
  }

  // --- 属性管理 ---
  async function addProperty(title) {
    var name = document.getElementById('kbPropName').value.trim();
    var value = document.getElementById('kbPropValue').value.trim();
    var type = document.getElementById('kbPropType').value;
    if (!name) { alert('Property name required'); return; }
    var parsed = value;
    if (type === 'number') parsed = parseFloat(value);
    else if (type === 'boolean') parsed = value === 'true';
    else if (type === 'tags') parsed = value.split(',').map(function(s){return s.trim();}).filter(Boolean);
    await apiPut('/pages/' + encodeURIComponent(title) + '/properties', { name: name, value: parsed, type: type });
    loadProperties(title);
  }

  async function deleteProperty(title, name) {
    await apiDelete('/pages/' + encodeURIComponent(title) + '/properties/' + encodeURIComponent(name));
    loadProperties(title);
  }

  // --- 未链接引用 ---
  async function showUnlinked(title) {
    var data = await apiGet('/pages/' + encodeURIComponent(title) + '/unlinked');
    var refs = (data && data.references) || [];
    var html = '<div class="kb-suggest-overlay" id="kbUnlinkedOverlay">' +
      '<div class="kb-suggest-modal">' +
        '<div class="kb-suggest-header"><h3>Unlinked References to "' + esc(title) + '"</h3><button onclick="document.getElementById(\'kbUnlinkedOverlay\').remove()">✕</button></div>' +
        '<div class="kb-suggest-body">';
    if (!refs.length) {
      html += '<p class="kb-muted">No unlinked references found.</p>';
    } else {
      html += refs.map(function(r) {
        return '<div class="kb-backlink-item">' +
          '<a class="kb-wikilink" href="#" onclick="KB.openPage(\'' + esc(r.page_title).replace(/'/g,"\\'") + '\');document.getElementById(\'kbUnlinkedOverlay\').remove();return false;">' + esc(r.page_title) + '</a>' +
          '<span class="kb-backlink-snippet">' + esc(r.context || r.block_content || '') + '</span>' +
          '</div>';
      }).join('');
    }
    html += '</div></div></div>';
    document.body.insertAdjacentHTML('beforeend', html);
  }

  // --- 导出 ---
  function exportPage(title, format) {
    if (format === 'html') {
      window.open('/api/kb/export/' + encodeURIComponent(title) + '/html', '_blank');
    } else if (format === 'json') {
      window.open('/api/kb/export/json', '_blank');
    }
  }

  // --- 回收站 ---
  async function showRecycle() {
    var data = await apiGet('/recycle');
    var items = (data && data.items) || [];
    var html = '<div class="kb-suggest-overlay" id="kbRecycleOverlay">' +
      '<div class="kb-suggest-modal">' +
        '<div class="kb-suggest-header"><h3>Recycle Bin</h3><button onclick="document.getElementById(\'kbRecycleOverlay\').remove()">✕</button></div>' +
        '<div class="kb-suggest-body">';
    if (!items.length) {
      html += '<p class="kb-muted">Recycle bin is empty.</p>';
    } else {
      html += items.map(function(it) {
        return '<div class="kb-recycle-item">' +
          '<div class="kb-recycle-title">' + esc(it.title) + '</div>' +
          '<div class="kb-recycle-date">Deleted: ' + esc(it.deleted_at) + '</div>' +
          '<button class="btn-sm" onclick="KB.restorePage(' + it.id + ')">Restore</button> ' +
          '<button class="btn-sm danger" onclick="KB.purgePage(' + it.id + ')">Delete Forever</button>' +
          '</div>';
      }).join('');
      html += '<div class="kb-recycle-actions"><button class="btn-sm danger" onclick="KB.emptyRecycle()">Empty Recycle Bin</button></div>';
    }
    html += '</div></div></div>';
    document.body.insertAdjacentHTML('beforeend', html);
  }

  async function restorePage(id) {
    await apiPost('/recycle/' + id + '/restore', {});
    document.getElementById('kbRecycleOverlay').remove();
    loadPages();
  }

  async function purgePage(id) {
    if (!confirm('Permanently delete this page? This cannot be undone.')) return;
    await apiDelete('/recycle/' + id);
    showRecycle();
  }

  async function emptyRecycle() {
    if (!confirm('Empty the entire recycle bin? This cannot be undone.')) return;
    await apiDelete('/recycle');
    showRecycle();
  }

  // --- 侧边栏切换 ---
  async function switchSidebarTab(tab) {
    sidebarTab = tab;
    // 更新 tab 激活状态
    document.querySelectorAll('.kb-tab').forEach(function(t) {
      t.classList.toggle('active', t.dataset.tab === tab);
    });
    var el = document.getElementById('kbPageList');
    if (tab === 'favorites') {
      var data = await apiGet('/favorites');
      renderPageList((data && data.pages) || []);
    } else if (tab === 'recent') {
      var rdata = await apiGet('/recent');
      renderPageList((rdata && rdata.pages) || []);
    } else {
      loadPages();
    }
  }

  // --- 知识图谱问答 ---
  async function showGraphQA() {
    var html = '<div class="kb-suggest-overlay" id="kbGraphQAOverlay">' +
      '<div class="kb-suggest-modal">' +
        '<div class="kb-suggest-header"><h3>🧠 Knowledge Graph Q&A</h3><button onclick="document.getElementById(\'kbGraphQAOverlay\').remove()">✕</button></div>' +
        '<div class="kb-suggest-body">' +
          '<p class="kb-muted">Ask questions about your knowledge base structure, connectivity, and health.</p>' +
          '<div class="kb-qa-input-row">' +
            '<input type="text" id="kbGraphQAInput" class="kb-qa-input" placeholder="e.g. 我的知识库结构健康吗？哪些页面需要更多链接？">' +
            '<button class="btn-sm primary" onclick="KB.askGraphQA()">Ask</button>' +
          '</div>' +
          '<div id="kbGraphQAAnswer"></div>' +
        '</div>' +
      '</div></div>';
    document.body.insertAdjacentHTML('beforeend', html);
  }

  async function askGraphQA() {
    var q = document.getElementById('kbGraphQAInput').value.trim();
    if (!q) return;
    var ansEl = document.getElementById('kbGraphQAAnswer');
    ansEl.innerHTML = '<div class="kb-qa-thinking">🤔 Analyzing knowledge graph...</div>';
    try {
      var data = await apiPost('/graph/qa', { question: q });
      if (data && data.answer) {
        ansEl.innerHTML = '<div class="kb-ai-answer">' + renderMd(data.answer) + '</div>';
      } else if (data && data.error) {
        ansEl.innerHTML = '<div class="kb-error">' + esc(data.error) + '</div>';
      }
    } catch(e) {
      ansEl.innerHTML = '<div class="kb-error">Failed: ' + esc(e.message) + '</div>';
    }
  }

  // --- 自动整理 ---
  async function showAutoOrganize() {
    var html = '<div class="kb-suggest-overlay" id="kbOrganizeOverlay">' +
      '<div class="kb-suggest-modal">' +
        '<div class="kb-suggest-header"><h3>🗂️ Auto Organize</h3><button onclick="document.getElementById(\'kbOrganizeOverlay\').remove()">✕</button></div>' +
        '<div class="kb-suggest-body"><div class="kb-qa-thinking">Analyzing pages and suggesting organization...</div></div>' +
      '</div></div>';
    document.body.insertAdjacentHTML('beforeend', html);
    var data = await apiPost('/auto-organize', {});
    var body = document.querySelector('#kbOrganizeOverlay .kb-suggest-body');
    if (!data || !data.suggestion) {
      body.innerHTML = '<div class="kb-error">Failed to analyze</div>';
      return;
    }
    var s = data.suggestion;
    var html2 = '';
    if (s.categories && s.categories.length) {
      html2 += '<h4>📂 Suggested Categories</h4>';
      html2 += s.categories.map(function(c) {
        return '<div class="kb-organize-cat"><strong>' + esc(c.name) + '</strong><div class="kb-muted">' +
          (c.pages || []).map(function(p) { return esc(p); }).join(', ') + '</div></div>';
      }).join('');
    }
    if (s.tag_suggestions && s.tag_suggestions.length) {
      html2 += '<h4>🏷️ Tag Suggestions</h4>';
      html2 += s.tag_suggestions.map(function(t) {
        return '<div class="kb-organize-tag"><strong>' + esc(t.page) + '</strong>: ' +
          (t.tags || []).map(function(tg) { return '<span class="kb-tag-sm">#' + esc(tg) + '</span>'; }).join(' ') + '</div>';
      }).join('');
    }
    if (s.merge_suggestions && s.merge_suggestions.length) {
      html2 += '<h4>🔀 Merge Suggestions</h4>';
      html2 += s.merge_suggestions.map(function(m) {
        return '<div class="kb-organize-merge"><strong>' + (m.pages || []).join(' + ') + '</strong><div class="kb-muted">' + esc(m.reason) + '</div></div>';
      }).join('');
    }
    if (s.raw_analysis && !html2) {
      html2 = '<div>' + renderMd(s.raw_analysis) + '</div>';
    }
    if (!html2) html2 = '<p class="kb-muted">No suggestions available.</p>';
    body.innerHTML = html2;
  }

  // --- 查询构建器 ---
  function showQueryBuilder() {
    var html = '<div class="kb-suggest-overlay" id="kbQueryOverlay">' +
      '<div class="kb-suggest-modal kb-query-modal">' +
        '<div class="kb-suggest-header"><h3>🔍 Query Builder</h3><button onclick="document.getElementById(\'kbQueryOverlay\').remove()">✕</button></div>' +
        '<div class="kb-suggest-body">' +
          '<div class="kb-query-help">' +
            '<p class="kb-muted">Supported predicates:</p>' +
            '<div class="kb-query-examples">' +
              '<code>(task TODO)</code> <code>(tag #work)</code> <code>(property status done)</code>' +
              '<code>(between -7d +0d)</code> <code>(content "keyword")</code> <code>(page-type journal)</code>' +
              '<code>(orphan)</code> <code>(hub 5)</code> <code>(created-in 7)</code> <code>(updated-in 3)</code>' +
            '</div>' +
            '<p class="kb-muted">Combine with: <code>(and ...)</code> <code>(or ...)</code> <code>(not ...)</code></p>' +
          '</div>' +
          '<textarea id="kbQueryInput" class="kb-query-textarea" placeholder="(and (task TODO) (tag #work))"></textarea>' +
          '<div class="kb-query-actions">' +
            '<label><input type="checkbox" id="kbQueryAggregate"> With Aggregation</label>' +
            '<button class="btn-sm primary" onclick="KB.runQuery()">Run Query</button>' +
          '</div>' +
          '<div id="kbQueryResult"></div>' +
        '</div>' +
      '</div></div>';
    document.body.insertAdjacentHTML('beforeend', html);
  }

  async function runQuery() {
    var q = document.getElementById('kbQueryInput').value.trim();
    if (!q) return;
    var agg = document.getElementById('kbQueryAggregate').checked;
    var resEl = document.getElementById('kbQueryResult');
    resEl.innerHTML = '<div class="kb-qa-thinking">Running query...</div>';
    try {
      var data = await apiPost('/query', { query: q, aggregate: agg });
      if (data && data.error) {
        resEl.innerHTML = '<div class="kb-error">' + esc(data.error) + '</div>';
        return;
      }
      var html = '';
      // 聚合统计
      if (agg && (data.by_tag || data.by_status || data.by_page)) {
        html += '<div class="kb-query-agg">';
        html += '<div class="kb-query-agg-row"><strong>' + (data.total_pages || 0) + '</strong> pages, <strong>' + (data.total_blocks || 0) + '</strong> blocks</div>';
        if (data.by_status && Object.keys(data.by_status).length) {
          html += '<div class="kb-query-agg-section"><span class="kb-muted">By Status:</span> ';
          Object.keys(data.by_status).forEach(function(k) {
            html += '<span class="kb-tag-sm">' + esc(k) + ': ' + data.by_status[k] + '</span>';
          });
          html += '</div>';
        }
        if (data.by_tag && Object.keys(data.by_tag).length) {
          html += '<div class="kb-query-agg-section"><span class="kb-muted">By Tag:</span> ';
          Object.keys(data.by_tag).forEach(function(k) {
            html += '<span class="kb-tag-sm">#' + esc(k) + ': ' + data.by_tag[k] + '</span>';
          });
          html += '</div>';
        }
        html += '</div>';
      }
      // 结果列表
      var blocks = data.blocks || [];
      if (blocks.length === 0) {
        html += '<p class="kb-muted">No matching blocks.</p>';
      } else {
        html += '<div class="kb-query-blocks">';
        blocks.slice(0, 50).forEach(function(b) {
          html += '<div class="kb-query-block">' + renderMd(b.content || '') + '</div>';
        });
        if (blocks.length > 50) {
          html += '<p class="kb-muted">... and ' + (blocks.length - 50) + ' more</p>';
        }
        html += '</div>';
      }
      resEl.innerHTML = html;
    } catch(e) {
      resEl.innerHTML = '<div class="kb-error">Failed: ' + esc(e.message) + '</div>';
    }
  }

  // --- Public API ---
  return {
    init: init,
    openPage: openPage,
    newPage: newPage,
    cancelNew: cancelNew,
    createPage: createPage,
    toggleEdit: toggleEdit,
    savePage: savePage,
    deletePage: deletePage,
    handleSearchKey: handleSearchKey,
    search: search,
    showTags: showTags,
    showPagesByTag: showPagesByTag,
    showGraph: showGraph,
    showQA: showQA,
    askQA: askQA,
    suggestSummary: suggestSummary,
    suggestLinks: suggestLinks,
    suggestTagsForEditor: suggestTagsForEditor,
    sync: sync,
    // 新增
    insertMd: insertMd,
    insertLinePrefix: insertLinePrefix,
    handleEditorKey: handleEditorKey,
    handleEditorInput: handleEditorInput,
    applySlash: applySlash,
    aiContinue: aiContinue,
    toggleFavorite: toggleFavorite,
    addProperty: addProperty,
    deleteProperty: deleteProperty,
    showUnlinked: showUnlinked,
    exportPage: exportPage,
    showRecycle: showRecycle,
    restorePage: restorePage,
    purgePage: purgePage,
    emptyRecycle: emptyRecycle,
    switchSidebarTab: switchSidebarTab,
    showGraphQA: showGraphQA,
    askGraphQA: askGraphQA,
    showAutoOrganize: showAutoOrganize,
    showQueryBuilder: showQueryBuilder,
    runQuery: runQuery
  };
})();
