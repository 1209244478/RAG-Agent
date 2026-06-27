// 命令面板（Command Palette）- Ctrl+K / Cmd+K 唤起
// 快速跳转页面、执行命令、插入模板
(function () {
  const CommandPalette = (function () {
    let isOpen = false;
    let overlay = null;
    let input = null;
    let resultsEl = null;
    let selectedIndex = 0;
    let currentResults = [];

    // 命令定义
    const commands = [
      {
        id: 'new-page',
        title: 'New Page',
        icon: '+',
        keywords: ['new', 'create', 'page', '新建'],
        action: function () { if (window.KB) KB.newPage(); }
      },
      {
        id: 'today-journal',
        title: 'Open Today Journal',
        icon: '📅',
        keywords: ['today', 'journal', '日记', '今日'],
        action: function () { openTodayJournal(); }
      },
      {
        id: 'search',
        title: 'Search Pages',
        icon: '🔍',
        keywords: ['search', 'find', '搜索', '查找'],
        action: function () {
          close();
          setTimeout(function () {
            const input = document.getElementById('kbSearchInput');
            if (input) input.focus();
          }, 100);
        }
      },
      {
        id: 'graph',
        title: 'View Knowledge Graph',
        icon: '🕸',
        keywords: ['graph', 'network', '图谱', '关系'],
        action: function () { if (window.KB) KB.showGraph(); }
      },
      {
        id: 'tags',
        title: 'View Tags',
        icon: '#',
        keywords: ['tag', 'tags', '标签'],
        action: function () { if (window.KB) KB.showTags(); }
      },
      {
        id: 'ask-ai',
        title: 'Ask AI',
        icon: '🤖',
        keywords: ['ai', 'ask', 'qa', 'question', '问答'],
        action: function () { if (window.KB) KB.showQA(); }
      },
      {
        id: 'tasks',
        title: 'View All Tasks',
        icon: '✓',
        keywords: ['task', 'todo', '任务', '待办'],
        action: function () { showTasks(); }
      },
      {
        id: 'import',
        title: 'Import Markdown Directory',
        icon: '📥',
        keywords: ['import', 'upload', '导入'],
        action: function () { showImportDialog(); }
      },
      {
        id: 'templates',
        title: 'Browse Templates',
        icon: '📋',
        keywords: ['template', '模板'],
        action: function () { showTemplates(); }
      },
      {
        id: 'fts-rebuild',
        title: 'Rebuild Search Index (FTS)',
        icon: '🔄',
        keywords: ['fts', 'index', 'rebuild', '索引'],
        action: function () { rebuildFTS(); }
      },
      {
        id: 'graph-qa',
        title: 'Knowledge Graph Q&A',
        icon: '🧠',
        keywords: ['graph', 'ai', 'qa', 'structure', 'health', '图谱问答'],
        action: function () { close(); if (window.KB) KB.showGraphQA(); }
      },
      {
        id: 'auto-organize',
        title: 'Auto Organize Knowledge Base',
        icon: '🗂️',
        keywords: ['organize', 'structure', 'categorize', '整理', '分类'],
        action: function () { close(); if (window.KB) KB.showAutoOrganize(); }
      },
      {
        id: 'recycle',
        title: 'Open Recycle Bin',
        icon: '🗑',
        keywords: ['recycle', 'trash', 'delete', 'restore', '回收站'],
        action: function () { close(); if (window.KB) KB.showRecycle(); }
      },
      {
        id: 'favorites',
        title: 'Show Favorites',
        icon: '★',
        keywords: ['favorite', 'star', '收藏'],
        action: function () { close(); if (window.KB) KB.switchSidebarTab('favorites'); }
      },
      {
        id: 'recent',
        title: 'Show Recent Pages',
        icon: '🕐',
        keywords: ['recent', 'history', '最近'],
        action: function () { close(); if (window.KB) KB.switchSidebarTab('recent'); }
      },
      {
        id: 'query-builder',
        title: 'Query Builder',
        icon: '🔍',
        keywords: ['query', 'dsl', 'search', 'advanced', '查询'],
        action: function () { close(); if (window.KB) KB.showQueryBuilder(); }
      }
    ];

    function init() {
      // 监听全局快捷键
      document.addEventListener('keydown', function (e) {
        if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
          e.preventDefault();
          toggle();
        }
        if (e.key === 'Escape' && isOpen) {
          close();
        }
      });
    }

    function toggle() {
      if (isOpen) {
        close();
      } else {
        open();
      }
    }

    function open() {
      if (isOpen) return;
      isOpen = true;
      render();
      setTimeout(function () {
        if (input) input.focus();
      }, 50);
    }

    function close() {
      isOpen = false;
      if (overlay) {
        overlay.remove();
        overlay = null;
      }
      input = null;
      resultsEl = null;
      selectedIndex = 0;
      currentResults = [];
    }

    function render() {
      close();
      overlay = document.createElement('div');
      overlay.className = 'cmd-palette-overlay';
      overlay.onclick = function (e) {
        if (e.target === overlay) close();
      };

      const palette = document.createElement('div');
      palette.className = 'cmd-palette';

      const inputWrap = document.createElement('div');
      inputWrap.className = 'cmd-palette-input-wrap';

      input = document.createElement('input');
      input.type = 'text';
      input.placeholder = 'Type a command or search pages... (Ctrl+K)';
      input.className = 'cmd-palette-input';
      input.oninput = function () { updateResults(); };
      input.onkeydown = function (e) { handleKeydown(e); };

      inputWrap.appendChild(input);
      palette.appendChild(inputWrap);

      resultsEl = document.createElement('div');
      resultsEl.className = 'cmd-palette-results';
      palette.appendChild(resultsEl);

      overlay.appendChild(palette);
      document.body.appendChild(overlay);

      updateResults();
    }

    function handleKeydown(e) {
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        selectedIndex = Math.min(selectedIndex + 1, currentResults.length - 1);
        renderResults();
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        selectedIndex = Math.max(selectedIndex - 1, 0);
        renderResults();
      } else if (e.key === 'Enter') {
        e.preventDefault();
        if (currentResults[selectedIndex]) {
          currentResults[selectedIndex].action();
          close();
        }
      }
    }

    function updateResults() {
      const query = (input.value || '').toLowerCase().trim();
      selectedIndex = 0;

      if (query === '') {
        // 显示所有命令
        currentResults = commands.slice();
        renderResults();
        return;
      }

      // 搜索命令
      const matchedCommands = commands.filter(function (cmd) {
        if (cmd.title.toLowerCase().indexOf(query) >= 0) return true;
        return cmd.keywords.some(function (k) { return k.indexOf(query) >= 0; });
      });

      // 搜索页面（异步）
      currentResults = matchedCommands;
      renderResults();

      if (window.KB && window.KB.api) {
        searchPages(query).then(function (pages) {
          // 在命令后追加页面结果
          pages.forEach(function (p) {
            currentResults.push({
              id: 'page-' + p.title,
              title: p.title,
              icon: '📄',
              keywords: [],
              action: function () {
                close();
                if (window.KB) KB.openPage(p.title);
              }
            });
          });
          renderResults();
        }).catch(function () {});
      }
    }

    function searchPages(query) {
      return fetch('/api/kb/fts/search?q=' + encodeURIComponent(query) + '&limit=10', {
        credentials: 'include'
      }).then(function (r) { return r.json(); })
        .then(function (data) {
          const pages = [];
          const seen = {};
          (data.results || []).forEach(function (r) {
            if (r.title && !seen[r.title]) {
              seen[r.title] = true;
              pages.push({ title: r.title, page_id: r.page_id });
            }
          });
          return pages;
        });
    }

    function renderResults() {
      if (!resultsEl) return;
      resultsEl.innerHTML = '';

      if (currentResults.length === 0) {
        const empty = document.createElement('div');
        empty.className = 'cmd-palette-empty';
        empty.textContent = 'No results found';
        resultsEl.appendChild(empty);
        return;
      }

      currentResults.forEach(function (item, idx) {
        const el = document.createElement('div');
        el.className = 'cmd-palette-item' + (idx === selectedIndex ? ' selected' : '');
        el.onclick = function () {
          item.action();
          close();
        };
        el.onmouseenter = function () {
          selectedIndex = idx;
          renderResults();
        };

        const icon = document.createElement('span');
        icon.className = 'cmd-palette-icon';
        icon.textContent = item.icon || '›';
        el.appendChild(icon);

        const title = document.createElement('span');
        title.className = 'cmd-palette-title';
        title.textContent = item.title;
        el.appendChild(title);

        resultsEl.appendChild(el);
      });
    }

    // --- 命令动作实现 ---

    function openTodayJournal() {
      fetch('/api/kb/journal/today', { credentials: 'include' })
        .then(function (r) { return r.json(); })
        .then(function (page) {
          close();
          if (window.KB && page && page.title) {
            KB.openPage(page.title);
          }
        })
        .catch(function (err) {
          alert('Failed to open journal: ' + err.message);
        });
    }

    function showTasks() {
      fetch('/api/kb/tasks', { credentials: 'include' })
        .then(function (r) { return r.json(); })
        .then(function (data) {
          close();
          renderTaskView(data.tasks || []);
        })
        .catch(function (err) {
          alert('Failed to load tasks: ' + err.message);
        });
    }

    function renderTaskView(tasks) {
      const content = document.getElementById('kbContent');
      if (!content) return;

      let html = '<div class="kb-tasks-view">';
      html += '<div class="kb-toolbar-bar"><h2>Tasks</h2>';
      html += '<button class="btn-sm" onclick="KB.init()">Back</button></div>';

      if (tasks.length === 0) {
        html += '<div class="empty-state"><p>No tasks found. Use TODO/DOING/DONE/LATER/NOW keywords in your pages.</p></div>';
      } else {
        const groups = { TODO: [], DOING: [], DONE: [], LATER: [], NOW: [] };
        tasks.forEach(function (t) {
          if (groups[t.status]) groups[t.status].push(t);
        });

        ['NOW', 'TODO', 'DOING', 'LATER', 'DONE'].forEach(function (status) {
          if (groups[status].length === 0) return;
          html += '<div class="kb-task-group"><h3>' + status + ' (' + groups[status].length + ')</h3>';
          groups[status].forEach(function (t) {
            html += '<div class="kb-task-item" data-block-id="' + t.block_id + '">';
            html += '<span class="kb-task-status kb-task-status-' + t.status.toLowerCase() + '">' + t.status + '</span>';
            html += '<span class="kb-task-content">' + escapeHtml(t.content) + '</span>';
            html += '<span class="kb-task-page">' + escapeHtml(t.page_title) + '</span>';
            html += '</div>';
          });
          html += '</div>';
        });
      }
      html += '</div>';
      content.innerHTML = html;
    }

    function showImportDialog() {
      const dir = prompt('Enter directory path to import Markdown files from:');
      if (!dir) { close(); return; }

      fetch('/api/kb/import', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ directory: dir })
      })
        .then(function (r) { return r.json(); })
        .then(function (result) {
          close();
          alert('Imported ' + result.imported + ' of ' + result.total_files + ' files.\n' +
            (result.errors && result.errors.length ? 'Errors:\n' + result.errors.slice(0, 5).join('\n') : ''));
          if (window.KB) KB.init();
        })
        .catch(function (err) {
          alert('Import failed: ' + err.message);
        });
    }

    function showTemplates() {
      fetch('/api/kb/templates', { credentials: 'include' })
        .then(function (r) { return r.json(); })
        .then(function (data) {
          close();
          renderTemplateView(data.templates || []);
        })
        .catch(function (err) {
          alert('Failed to load templates: ' + err.message);
        });
    }

    function renderTemplateView(templates) {
      const content = document.getElementById('kbContent');
      if (!content) return;

      let html = '<div class="kb-templates-view">';
      html += '<div class="kb-toolbar-bar"><h2>Templates</h2>';
      html += '<button class="btn-sm" onclick="KB.init()">Back</button></div>';

      if (templates.length === 0) {
        html += '<div class="empty-state"><p>No templates found. Create pages in "templates/" namespace.</p></div>';
      } else {
        html += '<div class="kb-template-list">';
        templates.forEach(function (t) {
          html += '<div class="kb-template-item" onclick="CommandPalette.applyTemplate(\'' + escapeHtml(t.title) + '\')">';
          html += '<span class="kb-template-icon">📋</span>';
          html += '<span class="kb-template-title">' + escapeHtml(t.title) + '</span>';
          html += '</div>';
        });
        html += '</div>';
      }
      html += '</div>';
      content.innerHTML = html;
    }

    function applyTemplate(name) {
      const pageTitle = prompt('Enter new page title:');
      if (!pageTitle) return;

      fetch('/api/kb/templates/apply', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ template: name, page_title: pageTitle, args: [] })
      })
        .then(function (r) { return r.json(); })
        .then(function (data) {
          if (data.content) {
            // 创建新页面并填充内容
            fetch('/api/kb/pages', {
              method: 'POST',
              credentials: 'include',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ title: pageTitle, content: data.content })
            })
              .then(function (r) { return r.json(); })
              .then(function () {
                if (window.KB) KB.openPage(pageTitle);
              });
          }
        })
        .catch(function (err) {
          alert('Apply template failed: ' + err.message);
        });
    }

    function rebuildFTS() {
      if (!confirm('Rebuild full-text search index? This may take a moment.')) return;
      fetch('/api/kb/fts/rebuild', {
        method: 'POST',
        credentials: 'include'
      })
        .then(function (r) { return r.json(); })
        .then(function (data) {
          close();
          alert('FTS index rebuilt successfully.');
        })
        .catch(function (err) {
          alert('Rebuild failed: ' + err.message);
        });
    }

    function escapeHtml(s) {
      if (!s) return '';
      return String(s).replace(/[&<>"']/g, function (c) {
        return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
      });
    }

    return {
      init: init,
      toggle: toggle,
      open: open,
      close: close,
      applyTemplate: applyTemplate
    };
  })();

  // 自动初始化
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', CommandPalette.init);
  } else {
    CommandPalette.init();
  }

  window.CommandPalette = CommandPalette;
})();
