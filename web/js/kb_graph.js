// kb_graph.js - Lightweight force-directed graph layout for knowledge graph
// Self-contained Verlet integration: repulsion (Coulomb) + attraction (Hooke) + centering.
// No external dependencies. Renders to SVG with drag/zoom/pan support.

var KBGraph = (function () {
  'use strict';

  // ==================== Force Simulation ====================
  function createSimulation(nodes, edges, options) {
    options = options || {};
    var width = options.width || 600;
    var height = options.height || 400;
    var alpha = 1;
    var alphaDecay = options.alphaDecay || 0.0228;
    var alphaMin = options.alphaMin || 0.001;
    var velocityDecay = options.velocityDecay || 0.4;

    // Forces
    var chargeStrength = options.chargeStrength || -120;  // repulsion
    var linkDistance = options.linkDistance || 80;
    var linkStrength = options.linkStrength || 0.3;
    var centerStrength = options.centerStrength || 0.02;
    var collideRadius = options.collideRadius || 18;

    // Init node positions (random near center if not set)
    var cx = width / 2, cy = height / 2;
    nodes.forEach(function (n, i) {
      if (n.x == null) {
        var angle = (i / nodes.length) * 2 * Math.PI;
        var r = 40 + Math.random() * 60;
        n.x = cx + r * Math.cos(angle);
        n.y = cy + r * Math.sin(angle);
      }
      if (n.vx == null) n.vx = 0;
      if (n.vy == null) n.vy = 0;
    });

    // Build link references
    var nodeMap = {};
    nodes.forEach(function (n) { nodeMap[n.id] = n; });
    var links = edges.map(function (e) {
      return {
        source: typeof e.source === 'object' ? e.source : nodeMap[e.source],
        target: typeof e.target === 'object' ? e.target : nodeMap[e.target]
      };
    }).filter(function (l) { return l.source && l.target; });

    function tick() {
      if (alpha < alphaMin) return false;
      alpha *= (1 - alphaDecay);

      // 1. Repulsion (Coulomb): all pairs
      for (var i = 0; i < nodes.length; i++) {
        var a = nodes[i];
        for (var j = i + 1; j < nodes.length; j++) {
          var b = nodes[j];
          var dx = b.x - a.x;
          var dy = b.y - a.y;
          var dist2 = dx * dx + dy * dy;
          if (dist2 < 0.01) {
            dx = (Math.random() - 0.5) * 0.1;
            dy = (Math.random() - 0.5) * 0.1;
            dist2 = dx * dx + dy * dy;
          }
          var dist = Math.sqrt(dist2);
          var force = chargeStrength / dist2;
          var fx = (dx / dist) * force;
          var fy = (dy / dist) * force;
          a.vx -= fx;
          a.vy -= fy;
          b.vx += fx;
          b.vy += fy;
        }
      }

      // 2. Attraction (Hooke): links
      for (var k = 0; k < links.length; k++) {
        var l = links[k];
        var dx = l.target.x - l.source.x;
        var dy = l.target.y - l.source.y;
        var dist = Math.sqrt(dx * dx + dy * dy) || 0.01;
        var force = (dist - linkDistance) * linkStrength;
        var fx = (dx / dist) * force;
        var fy = (dy / dist) * force;
        l.source.vx += fx;
        l.source.vy += fy;
        l.target.vx -= fx;
        l.target.vy -= fy;
      }

      // 3. Collision resolution
      for (var i2 = 0; i2 < nodes.length; i2++) {
        for (var j2 = i2 + 1; j2 < nodes.length; j2++) {
          var a2 = nodes[i2], b2 = nodes[j2];
          var dx2 = b2.x - a2.x;
          var dy2 = b2.y - a2.y;
          var dist2 = dx2 * dx2 + dy2 * dy2;
          var minDist = collideRadius * 2;
          if (dist2 < minDist * minDist && dist2 > 0.01) {
            var dist = Math.sqrt(dist2);
            var overlap = (minDist - dist) / 2;
            var ox = (dx2 / dist) * overlap;
            var oy = (dy2 / dist) * overlap;
            a2.x -= ox; a2.y -= oy;
            b2.x += ox; b2.y += oy;
          }
        }
      }

      // 4. Centering + velocity integration
      for (var m = 0; m < nodes.length; m++) {
        var n = nodes[m];
        // Centering force
        n.vx += (cx - n.x) * centerStrength * alpha;
        n.vy += (cy - n.y) * centerStrength * alpha;
        // Integrate
        n.vx *= velocityDecay;
        n.vy *= velocityDecay;
        n.x += n.vx;
        n.y += n.vy;
        // Bounds
        n.x = Math.max(collideRadius, Math.min(width - collideRadius, n.x));
        n.y = Math.max(collideRadius, Math.min(height - collideRadius, n.y));
      }

      return true;
    }

    function reheat() { alpha = 1; }
    function getAlpha() { return alpha; }
    function setAlpha(v) { alpha = v; }

    return {
      tick: tick,
      reheat: reheat,
      getAlpha: getAlpha,
      setAlpha: setAlpha,
      links: links
    };
  }

  // ==================== SVG Renderer ====================
  function render(container, nodes, edges, options) {
    options = options || {};
    var width = options.width || (container.clientWidth || 600);
    var height = options.height || 420;

    // Ensure minimum size
    if (width < 200) width = 600;
    if (height < 200) height = 420;

    var sim = createSimulation(nodes, edges, {
      width: width,
      height: height,
      chargeStrength: options.chargeStrength || -150,
      linkDistance: options.linkDistance || 90,
      centerStrength: 0.03
    });

    // Build SVG
    container.innerHTML = '';
    var svgNs = 'http://www.w3.org/2000/svg';
    var svg = document.createElementNS(svgNs, 'svg');
    svg.setAttribute('width', width);
    svg.setAttribute('height', height);
    svg.setAttribute('class', 'kb-graph-svg');
    svg.style.background = 'transparent';
    svg.style.cursor = 'grab';
    container.appendChild(svg);

    // Zoom/pan group
    var g = document.createElementNS(svgNs, 'g');
    svg.appendChild(g);

    // Defs for arrow markers (optional)
    var defs = document.createElementNS(svgNs, 'defs');
    g.appendChild(defs);

    // Edge layer
    var edgeG = document.createElementNS(svgNs, 'g');
    edgeG.setAttribute('class', 'kb-graph-edges');
    g.appendChild(edgeG);

    // Node layer
    var nodeG = document.createElementNS(svgNs, 'g');
    nodeG.setAttribute('class', 'kb-graph-nodes');
    g.appendChild(nodeG);

    // Create edge elements
    var edgeEls = sim.links.map(function (l) {
      var line = document.createElementNS(svgNs, 'line');
      line.setAttribute('stroke', 'var(--border)');
      line.setAttribute('stroke-width', '1');
      line.setAttribute('opacity', '0.5');
      edgeG.appendChild(line);
      return { el: line, link: l };
    });

    // Create node elements
    var nodeEls = nodes.map(function (n) {
      var group = document.createElementNS(svgNs, 'g');
      group.setAttribute('class', 'kb-graph-node');
      group.style.cursor = 'pointer';

      var r = Math.max(5, Math.min(18, (n.size || 1) * 2.2));
      var circle = document.createElementNS(svgNs, 'circle');
      circle.setAttribute('r', r);
      circle.setAttribute('fill', getNodeColor(n));
      circle.setAttribute('opacity', '0.85');
      circle.setAttribute('stroke', 'var(--surface)');
      circle.setAttribute('stroke-width', '1.5');
      group.appendChild(circle);

      // Label (show if few nodes or node is large)
      if (nodes.length <= 40 || (n.size || 0) >= 3) {
        var text = document.createElementNS(svgNs, 'text');
        text.setAttribute('x', 0);
        text.setAttribute('y', r + 12);
        text.setAttribute('text-anchor', 'middle');
        text.setAttribute('font-size', '10');
        text.setAttribute('fill', 'var(--text-2)');
        text.setAttribute('pointer-events', 'none');
        text.textContent = (n.label || '').slice(0, 22);
        group.appendChild(text);
      }

      // Hover effect
      group.addEventListener('mouseenter', function () {
        circle.setAttribute('opacity', '1');
        circle.setAttribute('stroke-width', '2.5');
      });
      group.addEventListener('mouseleave', function () {
        circle.setAttribute('opacity', '0.85');
        circle.setAttribute('stroke-width', '1.5');
      });

      // Click to open page
      if (n.label && options.onNodeClick) {
        group.addEventListener('click', function (e) {
          if (!group._dragged) options.onNodeClick(n);
        });
      }

      // Drag
      attachDrag(group, n, sim, svg);

      nodeG.appendChild(group);
      return { el: group, circle: circle, node: n };
    });

    // Animation loop
    var running = true;
    var frameCount = 0;
    function loop() {
      if (!running) return;
      var more = sim.tick();
      frameCount++;

      // Update positions
      edgeEls.forEach(function (e) {
        e.el.setAttribute('x1', e.link.source.x);
        e.el.setAttribute('y1', e.link.source.y);
        e.el.setAttribute('x2', e.link.target.x);
        e.el.setAttribute('y2', e.link.target.y);
      });
      nodeEls.forEach(function (ne) {
        ne.el.setAttribute('transform', 'translate(' + ne.node.x + ',' + ne.node.y + ')');
      });

      // Stop when settled
      if (!more && frameCount > 30) {
        running = false;
        return;
      }
      requestAnimationFrame(loop);
    }
    requestAnimationFrame(loop);

    // Pan/zoom on background
    attachPanZoom(svg, g, sim);

    return {
      stop: function () { running = false; },
      reheat: function () { sim.reheat(); running = true; requestAnimationFrame(loop); }
    };
  }

  function getNodeColor(n) {
    // Color by size: small=accent, large=highlight
    var size = n.size || 1;
    if (size >= 5) return 'var(--accent)';
    if (size >= 3) return 'var(--accent-hover)';
    return 'var(--accent-dim)';
  }

  // ==================== Drag ====================
  function attachDrag(group, node, sim, svg) {
    var dragging = false;
    var startX, startY;

    group.addEventListener('mousedown', function (e) {
      e.preventDefault();
      e.stopPropagation();
      dragging = true;
      group._dragged = false;
      startX = e.clientX;
      startY = e.clientY;
      sim.reheat();

      function onMove(ev) {
        if (!dragging) return;
        var dx = ev.clientX - startX;
        var dy = ev.clientY - startY;
        if (Math.abs(dx) > 3 || Math.abs(dy) > 3) group._dragged = true;

        // Convert screen to SVG coords
        var pt = svg.createSVGPoint();
        pt.x = ev.clientX;
        pt.y = ev.clientY;
        var ctm = svg.getScreenCTM();
        if (ctm) {
          var svgPt = pt.matrixTransform(ctm.inverse());
          node.x = svgPt.x;
          node.y = svgPt.y;
          node.vx = 0;
          node.vy = 0;
        }
      }

      function onUp() {
        dragging = false;
        document.removeEventListener('mousemove', onMove);
        document.removeEventListener('mouseup', onUp);
        // Reset dragged flag after click handler runs
        setTimeout(function () { group._dragged = false; }, 0);
      }

      document.addEventListener('mousemove', onMove);
      document.addEventListener('mouseup', onUp);
    });
  }

  // ==================== Pan / Zoom ====================
  function attachPanZoom(svg, g, sim) {
    var scale = 1;
    var tx = 0, ty = 0;
    var panning = false;
    var startX, startY, startTx, startTy;

    svg.addEventListener('mousedown', function (e) {
      // Only pan on background (not on nodes)
      if (e.target === svg || e.target.tagName === 'rect') {
        panning = true;
        startX = e.clientX;
        startY = e.clientY;
        startTx = tx;
        startTy = ty;
        svg.style.cursor = 'grabbing';
      }
    });

    document.addEventListener('mousemove', function (e) {
      if (!panning) return;
      tx = startTx + (e.clientX - startX);
      ty = startTy + (e.clientY - startY);
      applyTransform();
    });

    document.addEventListener('mouseup', function () {
      if (panning) {
        panning = false;
        svg.style.cursor = 'grab';
      }
    });

    svg.addEventListener('wheel', function (e) {
      e.preventDefault();
      var delta = e.deltaY > 0 ? 0.9 : 1.1;
      scale *= delta;
      scale = Math.max(0.2, Math.min(4, scale));
      applyTransform();
      sim.reheat();
    }, { passive: false });

    function applyTransform() {
      g.setAttribute('transform', 'translate(' + tx + ',' + ty + ') scale(' + scale + ')');
    }
  }

  // ==================== Public API ====================
  return {
    render: render,
    createSimulation: createSimulation
  };
})();
