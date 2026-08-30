// Re-renders server-emitted <time> elements in the reader's own timezone.
//
// The server has already formatted each one in the app's configured zone, so a
// browser without JavaScript still shows a sensible time; this only replaces
// that text with the reader's local equivalent. FORMATS must stay in sync with
// timeLayouts in internal/templates/templates.go -- a format the server emits
// but this file does not know leaves the fallback text in place.
//
// The "relative" format is the exception: it has no FORMATS entry because it is
// rendered as an elapsed span ("4 minutes ago") rather than a clock reading, and
// it is re-rendered on a timer so it does not go stale as the page sits open.
(function () {
    'use strict';

    const FORMATS = {
        datetime: { weekday: 'short', month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit', timeZoneName: 'short' },
        time: { hour: 'numeric', minute: '2-digit', timeZoneName: 'short' },
        date: { month: 'short', day: 'numeric' },
        mediumdate: { month: 'short', day: 'numeric', year: 'numeric' },
        fulldate: { month: 'long', day: 'numeric', year: 'numeric' },
        longdate: { weekday: 'long', month: 'long', day: 'numeric', year: 'numeric' },
        shortdatetime: { month: 'numeric', day: 'numeric', hour: 'numeric', minute: '2-digit' },
        mediumdatetime: { month: 'short', day: 'numeric', year: 'numeric', hour: 'numeric', minute: '2-digit' },
    };

    // Largest unit first: the first one the span reaches is the one used.
    const RELATIVE_UNITS = [
        ['day', 86400],
        ['hour', 3600],
        ['minute', 60],
        ['second', 1],
    ];

    const RELATIVE_SELECTOR = 'time[datetime][data-format="relative"]';
    const RELATIVE_REFRESH_MS = 30000;

    const relativeFormat = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' });

    // Renders the gap between now and the target: negative into the past
    // ("4 minutes ago"), positive into the future ("in 11 minutes").
    function renderRelative(el, parsed) {
        const seconds = (parsed.getTime() - Date.now()) / 1000;
        const magnitude = Math.abs(seconds);

        for (const [unit, size] of RELATIVE_UNITS) {
            if (magnitude >= size || size === 1) {
                el.textContent = relativeFormat.format(Math.round(seconds / size), unit);
                return;
            }
        }
    }

    function render(el) {
        const parsed = new Date(el.getAttribute('datetime'));
        if (Number.isNaN(parsed.getTime())) return;

        if (el.dataset.format === 'relative') {
            renderRelative(el, parsed);
            return;
        }

        const options = FORMATS[el.dataset.format];
        if (!options) return;

        el.textContent = parsed.toLocaleString(undefined, options);
    }

    function localize(root) {
        // The swapped-in node can itself be a <time>, which querySelectorAll
        // would not match.
        if (root.matches && root.matches('time[datetime][data-format]')) render(root);
        root.querySelectorAll('time[datetime][data-format]').forEach(render);
    }

    // "4 minutes ago" quietly becomes a lie on a page left open, so the relative
    // labels get re-rendered on a slow tick. Absolute ones never change and are
    // left alone.
    function refreshRelative() {
        document.querySelectorAll(RELATIVE_SELECTOR).forEach(render);
    }

    document.addEventListener('DOMContentLoaded', function () {
        localize(document);
        setInterval(refreshRelative, RELATIVE_REFRESH_MS);
    });

    // htmx fires htmx:load on every newly swapped node, and its events bubble to
    // document, so fragments get localized without re-scanning the whole page.
    document.addEventListener('htmx:load', function (event) {
        localize(event.target);
    });
})();
