// Minimal vanilla A2UI v0.9 renderer for the ycode canvas.
//
// Implements the op stream (createSurface / updateComponents /
// updateDataModel) over a small basic-catalog subset:
//
//   Text, Heading, Code, Stat, Card, Row, Column, List, Divider, Button
//
// What works (v1):
//   - Component tree rooted at component id "root"; createSurface allocates
//     a DOM container, updateComponents sets the tree, updateDataModel
//     patches the bound data and re-renders.
//   - Prop value can be either a literal (string/number/array/object) or
//     {path: "/json/pointer"}. Path is resolved against the current data
//     context — root data model by default; item data inside a List
//     template iteration.
//   - List instantiates {componentId, path} for each item in the array
//     at `path`. Inside the template, relative paths ("name") resolve
//     against the per-item context; absolute paths ("/items/0/name")
//     still resolve against the surface root.
//   - Buttons fire a state.mutate event with format=a2ui, the surface
//     id, and the declared action when clicked. v2 will route them
//     back to the agent.
//
// What doesn't (deferred to v1.5/v2):
//   - Editable inputs (TextInput, Select) bound back to the data model.
//   - Animations, transitions.
//   - Custom catalogs beyond the basic one.
//   - Theme tokens. v1 ships a single light theme tied to the canvas CSS.
//
// Output of A2UI.attach(root) is an object with one method, applyOps(ops).
// The canvas.js dispatcher feeds it ops; this module owns the DOM under
// `root` plus the per-surface state map.

(function (global) {
  'use strict';

  // ---- public api ---------------------------------------------------------

  function attach(rootEl, options) {
    const opts = options || {};
    const surfaces = new Map(); // surfaceId → SurfaceState
    const log = (opts.log || function () {});

    // emit is called when a Button's action triggers — caller wires
    // this to a state.mutate WS send. Optional; absence = read-only.
    const emit = opts.emit || function () {};

    function applyOps(ops, originLabel) {
      for (const op of ops || []) {
        if (op.createSurface) {
          ensureSurface(op.createSurface.surfaceId, originLabel);
        } else if (op.updateComponents) {
          const s = ensureSurface(op.updateComponents.surfaceId, originLabel);
          s.componentMap = indexComponents(op.updateComponents.components);
          rerender(s);
        } else if (op.updateDataModel) {
          const s = ensureSurface(op.updateDataModel.surfaceId, originLabel);
          patchDataModel(s, op.updateDataModel.path, op.updateDataModel.value);
          rerender(s);
        } else {
          log('a2ui: unknown op', op);
        }
      }
    }

    function ensureSurface(id, originLabel) {
      let s = surfaces.get(id);
      if (s) return s;
      const wrap = document.createElement('div');
      wrap.className = 'a2ui-surface';
      wrap.dataset.surfaceId = id;
      const header = document.createElement('div');
      header.className = 'a2ui-surface-header';
      const idEl = document.createElement('span');
      idEl.className = 'a2ui-surface-id';
      idEl.textContent = 'a2ui: ' + id;
      header.appendChild(idEl);
      if (originLabel) {
        const o = document.createElement('span');
        o.className = 'widget-origin';
        o.textContent = 'via ' + originLabel;
        header.appendChild(o);
      }
      wrap.appendChild(header);
      const body = document.createElement('div');
      body.className = 'a2ui-body';
      wrap.appendChild(body);
      rootEl.appendChild(wrap);

      s = {
        id: id,
        body: body,
        componentMap: {},
        data: {},
        emit: emit,
      };
      surfaces.set(id, s);
      return s;
    }

    function indexComponents(components) {
      const m = {};
      for (const c of components || []) {
        if (c && c.id) m[c.id] = c;
      }
      return m;
    }

    function rerender(s) {
      const root = s.componentMap['root'];
      s.body.innerHTML = '';
      if (!root) {
        const placeholder = document.createElement('div');
        placeholder.className = 'a2ui-empty';
        placeholder.textContent = 'Waiting for a component with id="root"…';
        s.body.appendChild(placeholder);
        return;
      }
      const ctx = { data: s.data, rootData: s.data, surface: s };
      const el = renderComponent(root, s, ctx);
      if (el) s.body.appendChild(el);
    }

    return { applyOps: applyOps };
  }

  // ---- rendering ----------------------------------------------------------

  // renderComponent walks the component tree producing DOM. ctx carries
  // the data context: ctx.data is the value at the current path (root or
  // a List item); ctx.rootData is the surface's full data model used to
  // resolve absolute paths from inside a List template.
  function renderComponent(c, s, ctx) {
    if (!c || !c.component) return null;
    switch (c.component) {
      case 'Text':       return renderText(c, ctx, 'p', 'a2ui-text');
      case 'Heading':    return renderText(c, ctx, headingTag(c.level), 'a2ui-heading');
      case 'Caption':    return renderText(c, ctx, 'span', 'a2ui-caption');
      case 'Code':       return renderText(c, ctx, 'code', 'a2ui-code');
      case 'Divider':    return tag('hr', 'a2ui-divider');
      case 'Stat':       return renderStat(c, ctx);
      case 'Card':       return renderContainer(c, s, ctx, 'a2ui-card', /* multi */ false);
      case 'Row':        return renderContainer(c, s, ctx, 'a2ui-row ' + justifyClass(c, 'row'), /* multi */ true);
      case 'Column':     return renderContainer(c, s, ctx, 'a2ui-column ' + justifyClass(c, 'col'), /* multi */ true);
      case 'List':       return renderList(c, s, ctx);
      case 'Button':     return renderButton(c, s, ctx);
      default:
        // Unknown component — render a labeled placeholder so missing
        // catalog entries are visible rather than silently dropped.
        const el = tag('div', 'a2ui-unknown');
        el.textContent = '? ' + c.component;
        return el;
    }
  }

  function renderText(c, ctx, tagName, className) {
    const el = document.createElement(tagName);
    el.className = className;
    const value = resolve(c.text, ctx);
    el.textContent = (value === undefined || value === null) ? '' : String(value);
    return el;
  }

  function renderStat(c, ctx) {
    const el = tag('div', 'a2ui-stat');
    const label = tag('span', 'a2ui-stat-label');
    label.textContent = String(resolve(c.label, ctx) || '');
    const value = tag('span', 'a2ui-stat-value');
    value.textContent = String(resolve(c.value, ctx) || '');
    el.appendChild(label);
    el.appendChild(value);
    return el;
  }

  function renderContainer(c, s, ctx, className, multi) {
    const el = tag('div', className);
    if (multi) {
      const children = Array.isArray(c.children) ? c.children : [];
      for (const ref of children) {
        const child = lookupChild(ref, s);
        if (child) {
          const sub = renderComponent(child, s, ctx);
          if (sub) el.appendChild(sub);
        }
      }
    } else {
      const childRef = c.child !== undefined ? c.child : c.children;
      const child = lookupChild(childRef, s);
      if (child) {
        const sub = renderComponent(child, s, ctx);
        if (sub) el.appendChild(sub);
      }
    }
    return el;
  }

  function renderList(c, s, ctx) {
    const el = tag('div', 'a2ui-list ' + (c.direction === 'horizontal' ? 'a2ui-list-h' : 'a2ui-list-v'));
    const childRef = c.children;
    if (!childRef || !childRef.componentId || !childRef.path) {
      el.appendChild(error('List missing children.{componentId, path}'));
      return el;
    }
    const items = resolveData(childRef.path, ctx);
    if (!Array.isArray(items)) {
      // Render empty silently — common case: data hasn't loaded yet.
      return el;
    }
    const template = s.componentMap[childRef.componentId];
    if (!template) {
      el.appendChild(error('List template "' + childRef.componentId + '" not in component map'));
      return el;
    }
    items.forEach((item, idx) => {
      const itemCtx = {
        data: item,
        rootData: ctx.rootData,
        surface: s,
      };
      const sub = renderComponent(template, s, itemCtx);
      if (sub) {
        sub.dataset.listIdx = String(idx);
        el.appendChild(sub);
      }
    });
    return el;
  }

  function renderButton(c, s, ctx) {
    const btn = document.createElement('button');
    btn.className = 'a2ui-button a2ui-button-' + (c.variant || 'default');
    btn.type = 'button';
    btn.textContent = String(resolve(c.text, ctx) || c.label || 'Button');
    btn.addEventListener('click', (e) => {
      e.preventDefault();
      if (!c.action || !c.action.event) return;
      // Resolve any path bindings in the action context payload.
      const evt = c.action.event;
      const resolvedCtx = {};
      if (evt.context) {
        for (const k in evt.context) {
          resolvedCtx[k] = resolve(evt.context[k], ctx);
        }
      }
      s.emit({
        surface: s.id,
        action: evt.name,
        context: resolvedCtx,
      });
    });
    return btn;
  }

  // ---- value resolution ---------------------------------------------------

  // resolve evaluates a prop value: literal → returned as-is;
  // {path: "/foo/bar"} → looked up in the data context.
  function resolve(propValue, ctx) {
    if (propValue && typeof propValue === 'object' && typeof propValue.path === 'string') {
      return resolveData(propValue.path, ctx);
    }
    return propValue;
  }

  // resolveData walks a JSON-pointer-ish path through ctx.data (or
  // ctx.rootData when the path starts with /). The trailing / is the
  // root sentinel; "" is treated the same.
  function resolveData(path, ctx) {
    if (path === '' || path === '/') return ctx.data;
    const isAbsolute = path.charAt(0) === '/';
    const base = isAbsolute ? ctx.rootData : ctx.data;
    const segments = (isAbsolute ? path.slice(1) : path).split('/').filter(Boolean);
    let cur = base;
    for (const seg of segments) {
      if (cur === null || cur === undefined) return undefined;
      cur = cur[seg];
    }
    return cur;
  }

  function patchDataModel(s, path, value) {
    if (!path || path === '/' || path === '') {
      s.data = (value === undefined || value === null) ? {} : value;
      return;
    }
    const segments = path.replace(/^\//, '').split('/').filter(Boolean);
    if (segments.length === 0) {
      s.data = value;
      return;
    }
    if (s.data === null || typeof s.data !== 'object') s.data = {};
    let cur = s.data;
    for (let i = 0; i < segments.length - 1; i++) {
      const seg = segments[i];
      if (cur[seg] === null || typeof cur[seg] !== 'object') cur[seg] = {};
      cur = cur[seg];
    }
    cur[segments[segments.length - 1]] = value;
  }

  // ---- helpers ------------------------------------------------------------

  function lookupChild(ref, s) {
    if (!ref) return null;
    if (typeof ref === 'string') return s.componentMap[ref] || null;
    if (typeof ref === 'object' && ref.componentId) return s.componentMap[ref.componentId] || null;
    return null;
  }

  function tag(name, className) {
    const el = document.createElement(name);
    if (className) el.className = className;
    return el;
  }

  function error(msg) {
    const el = tag('div', 'a2ui-error');
    el.textContent = msg;
    return el;
  }

  function headingTag(level) {
    const n = Math.max(1, Math.min(6, parseInt(level, 10) || 2));
    return 'h' + n;
  }

  function justifyClass(c, axis) {
    const j = c.justify || '';
    const a = c.align || '';
    const parts = [];
    if (j) parts.push('a2ui-' + axis + '-j-' + j);
    if (a) parts.push('a2ui-' + axis + '-a-' + a);
    return parts.join(' ');
  }

  // Expose as a single global. Module-style imports aren't worth the
  // complexity for the canvas surface — canvas.js is a plain script and
  // loads this one via <script src=…> before its own code.
  global.A2UI = { attach: attach };
})(window);
