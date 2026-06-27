// kb_editor.js - Block-based contenteditable rich text editor for knowledge base
// Storage format: Markdown. Editing: WYSIWYG block editor.
// Each block is a contenteditable element (paragraph/heading/list/code/etc).

var KBEditor = (function () {
  'use strict';

  // ==================== Block types ====================
  // p, h1, h2, h3, ul, ol, todo, quote, code, divider

  // ==================== Markdown -> Blocks ====================
  function parseMarkdown(md) {
    var lines = (md || '').replace(/\r\n/g, '\n').split('\n');
    var blocks = [];
    var i = 0;
    while (i < lines.length) {
      var line = lines[i];

      // Code block
      if (/^```/.test(line)) {
        var lang = line.replace(/^```/, '').trim();
        var codeLines = [];
        i++;
        while (i < lines.length && !/^```/.test(lines[i])) {
          codeLines.push(lines[i]);
          i++;
        }
        i++; // skip closing ```
        blocks.push({ type: 'code', lang: lang, text: codeLines.join('\n') });
        continue;
      }

      // Divider
      if (/^---+\s*$/.test(line)) {
        blocks.push({ type: 'divider' });
        i++;
        continue;
      }

      // Heading
      var hm = line.match(/^(#{1,3})\s+(.+)$/);
      if (hm) {
        blocks.push({ type: 'h' + hm[1].length, text: hm[2] });
        i++;
        continue;
      }

      // Todo
      var tm = line.match(/^- \[([ xX])\]\s+(.+)$/);
      if (tm) {
        blocks.push({ type: 'todo', checked: tm[1].toLowerCase() === 'x', text: tm[2] });
        i++;
        continue;
      }

      // Quote
      var qm = line.match(/^>\s+(.*)$/);
      if (qm) {
        blocks.push({ type: 'quote', text: qm[1] });
        i++;
        continue;
      }

      // Ordered list
      var om = line.match(/^\d+\.\s+(.+)$/);
      if (om) {
        var items = [];
        while (i < lines.length && /^\d+\.\s+/.test(lines[i])) {
          items.push(lines[i].replace(/^\d+\.\s+/, ''));
          i++;
        }
        blocks.push({ type: 'ol', items: items });
        continue;
      }

      // Unordered list
      var um = line.match(/^-\s+(.+)$/);
      if (um) {
        var ulItems = [];
        while (i < lines.length && /^-\s+/.test(lines[i]) && !/^- \[/.test(lines[i])) {
          ulItems.push(lines[i].replace(/^-\s+/, ''));
          i++;
        }
        blocks.push({ type: 'ul', items: ulItems });
        continue;
      }

      // Empty line -> skip (paragraph separation handled by blocks)
      if (line.trim() === '') {
        i++;
        continue;
      }

      // Paragraph (collect consecutive non-empty, non-special lines)
      var paraLines = [];
      while (i < lines.length &&
        lines[i].trim() !== '' &&
        !/^```/.test(lines[i]) &&
        !/^#{1,3}\s/.test(lines[i]) &&
        !/^-\s/.test(lines[i]) &&
        !/^\d+\.\s/.test(lines[i]) &&
        !/^>\s/.test(lines[i]) &&
        !/^---+\s*$/.test(lines[i])) {
        paraLines.push(lines[i]);
        i++;
      }
      blocks.push({ type: 'p', text: paraLines.join(' ') });
    }
    return blocks;
  }

  // ==================== Inline Markdown -> HTML ====================
  function inlineMdToHtml(text) {
    if (!text) return '';
    var h = escapeHtml(text);
    // Wiki links [[Page Name]]
    h = h.replace(/\[\[([^\]]+)\]\]/g, function (m, name) {
      return '<a class="kb-wikilink" data-page="' + escapeAttr(name) + '">' + escapeHtml(name) + '</a>';
    });
    // Tags #tag
    h = h.replace(/(^|\s)#(\w[\w-]*)/g, function (m, pre, tag) {
      return pre + '<span class="kb-tag" data-tag="' + escapeAttr(tag) + '">#' + escapeHtml(tag) + '</span>';
    });
    // Bold
    h = h.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
    // Italic
    h = h.replace(/\*(.+?)\*/g, '<em>$1</em>');
    // Strikethrough
    h = h.replace(/~~(.+?)~~/g, '<s>$1</s>');
    // Inline code
    h = h.replace(/`([^`]+)`/g, '<code>$1</code>');
    return h;
  }

  // ==================== HTML -> Inline Markdown ====================
  function htmlToInlineMd(html) {
    if (!html) return '';
    // Create a temp element to walk the DOM tree
    var tmp = document.createElement('div');
    tmp.innerHTML = html;
    return nodeToMd(tmp);
  }

  function nodeToMd(node) {
    var result = '';
    for (var i = 0; i < node.childNodes.length; i++) {
      var child = node.childNodes[i];
      if (child.nodeType === Node.TEXT_NODE) {
        result += child.textContent;
      } else if (child.nodeType === Node.ELEMENT_NODE) {
        var tag = child.tagName.toLowerCase();
        var inner = nodeToMd(child);
        switch (tag) {
          case 'strong':
          case 'b':
            result += '**' + inner + '**';
            break;
          case 'em':
          case 'i':
            result += '*' + inner + '*';
            break;
          case 's':
          case 'strike':
            result += '~~' + inner + '~~';
            break;
          case 'code':
            result += '`' + child.textContent + '`';
            break;
          case 'a':
            if (child.classList.contains('kb-wikilink')) {
              var page = child.getAttribute('data-page') || child.textContent;
              result += '[[' + page + ']]';
            } else {
              result += inner;
            }
            break;
          case 'span':
            if (child.classList.contains('kb-tag')) {
              var tag2 = child.getAttribute('data-tag') || child.textContent.replace(/^#/, '');
              result += '#' + tag2;
            } else {
              result += inner;
            }
            break;
          case 'br':
            result += '\n';
            break;
          default:
            result += inner;
        }
      }
    }
    return result;
  }

  // ==================== Blocks -> DOM ====================
  function blocksToDom(blocks) {
    var container = document.createElement('div');
    container.className = 'kb-block-editor';
    blocks.forEach(function (b) {
      container.appendChild(blockToDom(b));
    });
    return container;
  }

  function blockToDom(b) {
    var el;
    switch (b.type) {
      case 'h1':
        el = document.createElement('div');
        el.className = 'kb-block kb-block-h1';
        el.setAttribute('data-block', 'h1');
        el.setAttribute('contenteditable', 'true');
        el.innerHTML = inlineMdToHtml(b.text);
        break;
      case 'h2':
        el = document.createElement('div');
        el.className = 'kb-block kb-block-h2';
        el.setAttribute('data-block', 'h2');
        el.setAttribute('contenteditable', 'true');
        el.innerHTML = inlineMdToHtml(b.text);
        break;
      case 'h3':
        el = document.createElement('div');
        el.className = 'kb-block kb-block-h3';
        el.setAttribute('data-block', 'h3');
        el.setAttribute('contenteditable', 'true');
        el.innerHTML = inlineMdToHtml(b.text);
        break;
      case 'quote':
        el = document.createElement('blockquote');
        el.className = 'kb-block kb-block-quote';
        el.setAttribute('data-block', 'quote');
        el.setAttribute('contenteditable', 'true');
        el.innerHTML = inlineMdToHtml(b.text);
        break;
      case 'code':
        el = document.createElement('div');
        el.className = 'kb-block kb-block-code';
        el.setAttribute('data-block', 'code');
        el.setAttribute('data-lang', b.lang || '');
        var pre = document.createElement('pre');
        pre.setAttribute('contenteditable', 'true');
        pre.textContent = b.text || '';
        el.appendChild(pre);
        break;
      case 'divider':
        el = document.createElement('div');
        el.className = 'kb-block kb-block-divider';
        el.setAttribute('data-block', 'divider');
        el.innerHTML = '<hr>';
        break;
      case 'todo':
        el = document.createElement('div');
        el.className = 'kb-block kb-block-todo';
        el.setAttribute('data-block', 'todo');
        var check = document.createElement('input');
        check.type = 'checkbox';
        check.checked = !!b.checked;
        check.onchange = function () {
          el.setAttribute('data-checked', check.checked ? 'x' : ' ');
        };
        el.appendChild(check);
        var span = document.createElement('span');
        span.setAttribute('contenteditable', 'true');
        span.className = 'kb-todo-text';
        span.innerHTML = inlineMdToHtml(b.text);
        el.appendChild(span);
        break;
      case 'ul':
        el = document.createElement('ul');
        el.className = 'kb-block kb-block-ul';
        el.setAttribute('data-block', 'ul');
        (b.items || []).forEach(function (item) {
          var li = document.createElement('li');
          li.setAttribute('contenteditable', 'true');
          li.innerHTML = inlineMdToHtml(item);
          el.appendChild(li);
        });
        break;
      case 'ol':
        el = document.createElement('ol');
        el.className = 'kb-block kb-block-ol';
        el.setAttribute('data-block', 'ol');
        (b.items || []).forEach(function (item) {
          var li = document.createElement('li');
          li.setAttribute('contenteditable', 'true');
          li.innerHTML = inlineMdToHtml(item);
          el.appendChild(li);
        });
        break;
      default: // paragraph
        el = document.createElement('div');
        el.className = 'kb-block kb-block-p';
        el.setAttribute('data-block', 'p');
        el.setAttribute('contenteditable', 'true');
        el.innerHTML = inlineMdToHtml(b.text);
    }
    return el;
  }

  // ==================== DOM -> Markdown ====================
  function domToMarkdown(container) {
    var blocks = [];
    var children = container.querySelectorAll(':scope > .kb-block, :scope > *');
    for (var i = 0; i < children.length; i++) {
      var el = children[i];
      var type = el.getAttribute('data-block') || 'p';
      blocks.push(domToBlock(el, type));
    }
    return blocksToMarkdown(blocks);
  }

  function domToBlock(el, type) {
    switch (type) {
      case 'h1':
      case 'h2':
      case 'h3':
      case 'quote':
        return { type: type, text: htmlToInlineMd(el.innerHTML) };
      case 'code':
        var pre = el.querySelector('pre');
        return { type: 'code', lang: el.getAttribute('data-lang') || '', text: pre ? pre.textContent : '' };
      case 'divider':
        return { type: 'divider' };
      case 'todo':
        var check = el.querySelector('input[type=checkbox]');
        var span = el.querySelector('.kb-todo-text');
        return { type: 'todo', checked: check ? check.checked : false, text: span ? htmlToInlineMd(span.innerHTML) : '' };
      case 'ul':
      case 'ol':
        var items = [];
        var lis = el.querySelectorAll('li');
        for (var j = 0; j < lis.length; j++) {
          items.push(htmlToInlineMd(lis[j].innerHTML));
        }
        return { type: type, items: items };
      default:
        return { type: 'p', text: htmlToInlineMd(el.innerHTML) };
    }
  }

  function blocksToMarkdown(blocks) {
    var lines = [];
    blocks.forEach(function (b) {
      switch (b.type) {
        case 'h1':
          lines.push('# ' + b.text);
          break;
        case 'h2':
          lines.push('## ' + b.text);
          break;
        case 'h3':
          lines.push('### ' + b.text);
          break;
        case 'quote':
          lines.push('> ' + b.text);
          break;
        case 'code':
          lines.push('```' + (b.lang || ''));
          lines.push(b.text);
          lines.push('```');
          break;
        case 'divider':
          lines.push('---');
          break;
        case 'todo':
          lines.push('- [' + (b.checked ? 'x' : ' ') + '] ' + b.text);
          break;
        case 'ul':
          (b.items || []).forEach(function (item) { lines.push('- ' + item); });
          break;
        case 'ol':
          (b.items || []).forEach(function (item, idx) { lines.push((idx + 1) + '. ' + item); });
          break;
        default:
          lines.push(b.text);
      }
      lines.push(''); // block separator
    });
    return lines.join('\n').replace(/\n{3,}/g, '\n\n').trim();
  }

  // ==================== Editor Mount ====================
  function mount(container, markdown) {
    var blocks = parseMarkdown(markdown || '');
    if (blocks.length === 0) {
      blocks = [{ type: 'p', text: '' }];
    }
    var dom = blocksToDom(blocks);
    container.innerHTML = '';
    container.appendChild(dom);
    attachBlockEvents(dom);
    return dom;
  }

  function getMarkdown(container) {
    var dom = container.querySelector('.kb-block-editor');
    if (!dom) return '';
    return domToMarkdown(dom);
  }

  // ==================== Block Events ====================
  function attachBlockEvents(container) {
    // Enter key: create new paragraph block
    container.addEventListener('keydown', function (e) {
      var target = e.target;
      if (!target.classList || !target.classList.contains('kb-block')) {
        // For li/todo span, handle Enter within list
        if (target.tagName === 'LI') {
          handleListEnter(e, target);
          return;
        }
        return;
      }
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        handleBlockEnter(e, target);
      }
      // Backspace at start of block: merge with previous
      if (e.key === 'Backspace' && getCaretOffset(target) === 0) {
        handleBlockBackspace(e, target);
      }
    });

    // Click wiki links
    container.addEventListener('click', function (e) {
      var link = e.target.closest('.kb-wikilink');
      if (link) {
        e.preventDefault();
        var page = link.getAttribute('data-page') || link.textContent;
        if (window.KB && KB.openPage) KB.openPage(page);
        return;
      }
      var tag = e.target.closest('.kb-tag');
      if (tag) {
        e.preventDefault();
        var t = tag.getAttribute('data-tag') || tag.textContent.replace(/^#/, '');
        if (window.KB && KB.showPagesByTag) KB.showPagesByTag(t);
      }
    });
  }

  function handleBlockEnter(e, block) {
    var type = block.getAttribute('data-block');
    // For code block, allow normal newline
    if (type === 'code') {
      // Let default behavior insert newline in pre
      return;
    }
    e.preventDefault();
    var newBlock = blockToDom({ type: 'p', text: '' });
    block.after(newBlock);
    newBlock.focus();
    // Place caret at start
    var range = document.createRange();
    range.selectNodeContents(newBlock);
    range.collapse(true);
    var sel = window.getSelection();
    sel.removeAllRanges();
    sel.addRange(range);
  }

  function handleListEnter(e, li) {
    // If li is empty, exit list
    if (li.textContent.trim() === '') {
      e.preventDefault();
      var list = li.closest('ul, ol');
      if (list) {
        var newP = blockToDom({ type: 'p', text: '' });
        list.after(newP);
        li.remove();
        // If list is now empty, remove it
        if (list.children.length === 0) list.remove();
        newP.focus();
        placeCaretAtStart(newP);
      }
    }
    // Otherwise let default create new <li>
  }

  function handleBlockBackspace(e, block) {
    if (block.textContent.trim() !== '') return;
    var prev = block.previousElementSibling;
    if (!prev) return;
    e.preventDefault();
    // Merge: focus end of previous block, remove current
    block.remove();
    prev.focus();
    placeCaretAtEnd(prev);
  }

  // ==================== Toolbar Actions ====================
  function execFormat(cmd, value) {
    document.execCommand(cmd, false, value || null);
  }

  function toggleBold() {
    execFormat('bold');
    focusBack();
  }

  function toggleItalic() {
    execFormat('italic');
    focusBack();
  }

  function toggleStrike() {
    execFormat('strikeThrough');
    focusBack();
  }

  function toggleCode() {
    // Wrap selection in <code>
    var sel = window.getSelection();
    if (!sel.rangeCount) return;
    var range = sel.getRangeAt(0);
    if (range.collapsed) {
      var code = document.createElement('code');
      code.textContent = 'code';
      range.insertNode(code);
      // Select the placeholder
      var newRange = document.createRange();
      newRange.selectNodeContents(code);
      sel.removeAllRanges();
      sel.addRange(newRange);
    } else {
      var text = range.toString();
      var code2 = document.createElement('code');
      code2.textContent = text;
      range.deleteContents();
      range.insertNode(code2);
    }
  }

  function insertWikiLink() {
    var sel = window.getSelection();
    if (!sel.rangeCount) return;
    var range = sel.getRangeAt(0);
    var text = range.toString() || 'Page Name';
    var a = document.createElement('a');
    a.className = 'kb-wikilink';
    a.setAttribute('data-page', text);
    a.textContent = text;
    range.deleteContents();
    range.insertNode(a);
    // Place caret after link
    range.setStartAfter(a);
    range.collapse(true);
    sel.removeAllRanges();
    sel.addRange(range);
  }

  function insertTag() {
    var sel = window.getSelection();
    if (!sel.rangeCount) return;
    var range = sel.getRangeAt(0);
    var text = range.toString() || 'tag';
    var span = document.createElement('span');
    span.className = 'kb-tag';
    span.setAttribute('data-tag', text);
    span.textContent = '#' + text;
    range.deleteContents();
    range.insertNode(span);
    range.setStartAfter(span);
    range.collapse(true);
    sel.removeAllRanges();
    sel.addRange(range);
  }

  // Convert current block to a different type
  function setBlockType(container, type) {
    var block = getCurrentBlock();
    if (!block) return;
    var text = htmlToInlineMd(block.innerHTML);
    if (block.tagName === 'BLOCKQUOTE') {
      text = htmlToInlineMd(block.innerHTML);
    }
    var newBlock = blockToDom({ type: type, text: text });
    block.replaceWith(newBlock);
    newBlock.focus();
    placeCaretAtEnd(newBlock);
  }

  function getCurrentBlock() {
    var sel = window.getSelection();
    if (!sel.rangeCount) return null;
    var node = sel.anchorNode;
    while (node && node.nodeType !== Node.ELEMENT_NODE) {
      node = node.parentNode;
    }
    while (node && !node.classList.contains('kb-block') && node.tagName !== 'LI') {
      node = node.parentNode;
      if (!node) return null;
    }
    return node;
  }

  // ==================== Slash Commands ====================
  var slashCommands = [
    { key: 'h1', label: 'Heading 1', type: 'h1' },
    { key: 'h2', label: 'Heading 2', type: 'h2' },
    { key: 'h3', label: 'Heading 3', type: 'h3' },
    { key: 'p', label: 'Paragraph', type: 'p' },
    { key: 'quote', label: 'Quote', type: 'quote' },
    { key: 'ul', label: 'Bullet List', type: 'ul' },
    { key: 'ol', label: 'Numbered List', type: 'ol' },
    { key: 'todo', label: 'Todo', type: 'todo' },
    { key: 'code', label: 'Code Block', type: 'code' },
    { key: 'divider', label: 'Divider', type: 'divider' },
    { key: 'link', label: '[[Page Link]]', action: 'link' },
    { key: 'tag', label: '#Tag', action: 'tag' },
    { key: 'ai', label: '✨ AI Continue', action: 'ai' }
  ];

  function setupSlashMenu(container) {
    var menu = document.createElement('div');
    menu.className = 'kb-slash-menu';
    menu.style.display = 'none';
    menu.id = 'kbEditorSlashMenu';
    container.appendChild(menu);

    container.addEventListener('input', function (e) {
      var block = e.target.closest('.kb-block');
      if (!block) return;
      handleSlashTrigger(block, menu);
    });

    container.addEventListener('keydown', function (e) {
      if (menu.style.display === 'none') return;
      if (e.key === 'Escape') {
        menu.style.display = 'none';
        return;
      }
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        navigateSlashMenu(menu, e.key === 'ArrowDown' ? 1 : -1);
      }
      if (e.key === 'Enter') {
        e.preventDefault();
        applySelectedSlash(menu);
      }
    });
  }

  function handleSlashTrigger(block, menu) {
    var sel = window.getSelection();
    if (!sel.rangeCount) return;
    var text = block.textContent;
    var caret = getCaretOffset(block);
    var before = text.substring(0, caret);
    var m = before.match(/\/(\w*)$/);
    if (!m) {
      menu.style.display = 'none';
      return;
    }
    var filter = m[1].toLowerCase();
    var matched = slashCommands.filter(function (c) {
      return c.label.toLowerCase().indexOf(filter) >= 0 || c.key.indexOf(filter) >= 0;
    });
    if (!matched.length) {
      menu.style.display = 'none';
      return;
    }
    renderSlashMenu(menu, matched, block, before.length - m[0].length);
  }

  function renderSlashMenu(menu, cmds, block, slashIdx) {
    menu.innerHTML = cmds.map(function (c, i) {
      return '<div class="kb-slash-item" data-idx="' + i + '">' + escapeHtml(c.label) + '</div>';
    }).join('');
    menu.style.display = 'block';
    menu.dataset.slashIdx = slashIdx;
    menu.dataset.cmds = JSON.stringify(cmds);
    menu.dataset.activeIdx = '0';
    // Highlight first
    var items = menu.querySelectorAll('.kb-slash-item');
    if (items.length) items[0].classList.add('active');

    // Position near caret
    var rect = block.getBoundingClientRect();
    menu.style.left = rect.left + 'px';
    menu.style.top = (rect.bottom + 4) + 'px';

    // Attach click handlers
    items.forEach(function (item, idx) {
      item.onmousedown = function (e) {
        e.preventDefault();
        menu.dataset.activeIdx = idx;
        applySelectedSlash(menu);
      };
    });
  }

  function navigateSlashMenu(menu, delta) {
    var items = menu.querySelectorAll('.kb-slash-item');
    if (!items.length) return;
    var cur = parseInt(menu.dataset.activeIdx || '0');
    cur = (cur + delta + items.length) % items.length;
    menu.dataset.activeIdx = cur;
    items.forEach(function (it, i) {
      it.classList.toggle('active', i === cur);
    });
  }

  function applySelectedSlash(menu) {
    var idx = parseInt(menu.dataset.activeIdx || '0');
    var cmds = JSON.parse(menu.dataset.cmds || '[]');
    var cmd = cmds[idx];
    if (!cmd) return;
    menu.style.display = 'none';

    var block = getCurrentBlock();
    if (!block) return;

    // Remove the "/xxx" text
    var slashIdx = parseInt(menu.dataset.slashIdx || '0');
    var sel = window.getSelection();
    var range = sel.getRangeAt(0);
    // Select from slashIdx to caret and delete
    var newRange = document.createRange();
    newRange.setStart(block.firstChild || block, 0);
    // Find text node at slashIdx
    setRangeAtOffset(block, slashIdx, range);
    range.deleteContents();

    if (cmd.action === 'link') {
      insertWikiLink();
    } else if (cmd.action === 'tag') {
      insertTag();
    } else if (cmd.action === 'ai') {
      if (window.KB && KB.aiContinue) KB.aiContinue();
    } else if (cmd.type) {
      setBlockType(null, cmd.type);
    }
  }

  // ==================== Helpers ====================
  function escapeHtml(s) {
    if (s == null) return '';
    return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }
  function escapeAttr(s) {
    return escapeHtml(s).replace(/'/g, '&#39;');
  }

  function getCaretOffset(element) {
    var caretOffset = 0;
    var doc = element.ownerDocument || document;
    var win = doc.defaultView || window;
    var sel = win.getSelection();
    if (sel.rangeCount > 0) {
      var range = sel.getRangeAt(0);
      var preRange = range.cloneRange();
      preRange.selectNodeContents(element);
      preRange.setEnd(range.endContainer, range.endOffset);
      caretOffset = preRange.toString().length;
    }
    return caretOffset;
  }

  function placeCaretAtStart(el) {
    var range = document.createRange();
    range.selectNodeContents(el);
    range.collapse(true);
    var sel = window.getSelection();
    sel.removeAllRanges();
    sel.addRange(range);
  }

  function placeCaretAtEnd(el) {
    var range = document.createRange();
    range.selectNodeContents(el);
    range.collapse(false);
    var sel = window.getSelection();
    sel.removeAllRanges();
    sel.addRange(range);
  }

  function setRangeAtOffset(el, offset, range) {
    var chars = 0;
    var done = false;
    function walk(node) {
      if (done) return;
      if (node.nodeType === Node.TEXT_NODE) {
        var next = chars + node.length;
        if (offset <= next) {
          range.setStart(node, offset - chars);
          range.setEnd(node, offset - chars);
          done = true;
        }
        chars = next;
      } else {
        for (var i = 0; i < node.childNodes.length; i++) {
          walk(node.childNodes[i]);
          if (done) return;
        }
      }
    }
    walk(el);
    if (!done) {
      range.selectNodeContents(el);
      range.collapse(false);
    }
  }

  function focusBack() {
    // Re-focus the editor after execCommand
    var sel = window.getSelection();
    if (!sel.rangeCount) {
      var editor = document.querySelector('.kb-block-editor');
      if (editor) {
        var last = editor.lastElementChild;
        if (last) last.focus();
      }
    }
  }

  // ==================== Public API ====================
  return {
    mount: mount,
    getMarkdown: getMarkdown,
    parseMarkdown: parseMarkdown,
    blocksToMarkdown: blocksToMarkdown,
    toggleBold: toggleBold,
    toggleItalic: toggleItalic,
    toggleStrike: toggleStrike,
    toggleCode: toggleCode,
    insertWikiLink: insertWikiLink,
    insertTag: insertTag,
    setBlockType: setBlockType,
    setupSlashMenu: setupSlashMenu,
    slashCommands: slashCommands
  };
})();
