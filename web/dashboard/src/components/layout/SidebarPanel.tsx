import React, { useState, useEffect } from 'react';
import { ChevronRight, ChevronLeft, ChevronDown, GripVertical, X } from 'lucide-react';

const SIDEBAR_EXPANDED_KEY = 'sidebarPanelExpanded';
const ACCORDION_STATE_KEY = 'sidebarAccordionState';

interface AccordionSection {
  id: string;
  title: string;
  badge?: React.ReactNode;
  headerActions?: React.ReactNode;
  content: React.ReactNode;
  /** Collapsed indicator shown when sidebar is collapsed */
  collapsedIndicator?: React.ReactNode;
}

interface SidebarPanelProps {
  sections: AccordionSection[];
  isExpanded: boolean;
  onToggle: () => void;
  width: number;
  isResizing: boolean;
  onResizeStart: (e: React.MouseEvent) => void;
}

export const SidebarPanel: React.FC<SidebarPanelProps> = ({
  sections,
  isExpanded,
  onToggle,
  width,
  isResizing,
  onResizeStart,
}) => {
  // Accordion open/closed state per section, persisted to localStorage
  const [openSections, setOpenSections] = useState<Record<string, boolean>>(() => {
    try {
      const saved = localStorage.getItem(ACCORDION_STATE_KEY);
      if (saved) return JSON.parse(saved);
    } catch { /* ignore */ }
    // Default: first section open, rest closed
    const defaults: Record<string, boolean> = {};
    sections.forEach((s, i) => { defaults[s.id] = i === 0; });
    return defaults;
  });

  useEffect(() => {
    localStorage.setItem(ACCORDION_STATE_KEY, JSON.stringify(openSections));
  }, [openSections]);

  const toggleSection = (id: string) => {
    setOpenSections(prev => ({ ...prev, [id]: !prev[id] }));
  };

  // Small screen detection for overlay mode
  const [isSmallScreen, setIsSmallScreen] = useState(false);
  useEffect(() => {
    const mq = window.matchMedia('(max-width: 768px)');
    setIsSmallScreen(mq.matches);
    const handler = (e: MediaQueryListEvent) => setIsSmallScreen(e.matches);
    mq.addEventListener('change', handler);
    return () => mq.removeEventListener('change', handler);
  }, []);

  // Collapsed view
  if (!isExpanded) {
    return (
      <div className="h-full flex flex-col items-center py-4 px-2 bg-white dark:bg-gray-800 border-r border-gray-200 dark:border-gray-700">
        <button
          onClick={onToggle}
          className="p-2.5 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors group"
          title="Expand sidebar"
        >
          <ChevronRight className="w-5 h-5 text-gray-500 dark:text-gray-400 group-hover:text-gray-700 dark:group-hover:text-gray-200" />
        </button>
        {/* Collapsed indicators from each section */}
        <div className="mt-3 flex flex-col items-center gap-4">
          {sections.map(section => (
            <React.Fragment key={section.id}>
              {section.collapsedIndicator}
            </React.Fragment>
          ))}
        </div>
      </div>
    );
  }

  // Overlay wrapper for small screens
  const panelContent = (
    <div
      className={`h-full flex bg-white dark:bg-gray-800 border-r border-gray-200 dark:border-gray-700 ${
        isSmallScreen ? 'w-full' : ''
      }`}
      style={isSmallScreen ? undefined : { width }}
    >
      <div className="flex-1 flex flex-col min-w-0">
        {/* Sidebar header with collapse button */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-gray-700">
          <span className="text-sm font-mono font-normal tracking-mono-wide text-gray-900 dark:text-gray-100">
            Sidebar
          </span>
          <button
            onClick={onToggle}
            className="p-2 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors group"
            title="Collapse sidebar"
          >
            {isSmallScreen ? (
              <X className="w-4 h-4 text-gray-500 dark:text-gray-400 group-hover:text-gray-700 dark:group-hover:text-gray-200" />
            ) : (
              <ChevronLeft className="w-4 h-4 text-gray-500 dark:text-gray-400 group-hover:text-gray-700 dark:group-hover:text-gray-200" />
            )}
          </button>
        </div>

        {/* Accordion sections */}
        <div className="flex-1 overflow-y-auto">
          {sections.map(section => {
            const isOpen = openSections[section.id] ?? false;
            return (
              <div key={section.id} className="border-b border-gray-200 dark:border-gray-700 last:border-b-0">
                {/* Section header */}
                <button
                  onClick={() => toggleSection(section.id)}
                  className="w-full flex items-center justify-between px-4 py-3 hover:bg-gray-50 dark:hover:bg-gray-700/50 transition-colors"
                >
                  <div className="flex items-center gap-2">
                    <ChevronDown
                      className={`w-4 h-4 text-gray-400 transition-transform ${isOpen ? '' : '-rotate-90'}`}
                    />
                    <span className="text-sm font-mono font-normal tracking-mono-wide text-gray-900 dark:text-gray-100">
                      {section.title}
                    </span>
                    {section.badge}
                  </div>
                  {/* Header actions (stop propagation to avoid toggling) */}
                  {isOpen && section.headerActions && (
                    <div onClick={e => e.stopPropagation()} className="flex items-center gap-1">
                      {section.headerActions}
                    </div>
                  )}
                </button>

                {/* Section content */}
                {isOpen && (
                  <div className="overflow-hidden">
                    {section.content}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>

      {/* Resize Handle (not on small screens) */}
      {!isSmallScreen && (
        <div
          onMouseDown={onResizeStart}
          className={`
            w-1.5 bg-gray-200 dark:bg-gray-700 cursor-ew-resize flex items-center justify-center
            hover:bg-gray-300 dark:hover:bg-gray-600 transition-colors group flex-shrink-0
            ${isResizing ? 'bg-blue-500 dark:bg-blue-600' : ''}
          `}
          title="Drag to resize"
        >
          <GripVertical
            className={`w-3 h-4 text-gray-400 group-hover:text-gray-600 dark:group-hover:text-gray-300 ${
              isResizing ? 'text-blue-200' : ''
            }`}
          />
        </div>
      )}
    </div>
  );

  // On small screens, render as overlay
  if (isSmallScreen) {
    return (
      <>
        {/* Backdrop */}
        <div
          className="fixed inset-0 bg-black/30 z-40"
          onClick={onToggle}
        />
        {/* Panel */}
        <div className="fixed inset-y-0 left-0 z-50 w-80 max-w-[85vw]">
          {panelContent}
        </div>
      </>
    );
  }

  return panelContent;
};
