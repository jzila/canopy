import React from 'react';
import { Check, X, Clock, Loader2, Wrench, AlertTriangle } from 'lucide-react';
import type { ValidationStatus } from '../../stores/stateStore';

interface ValidationStatusBadgeProps {
  status: ValidationStatus;
  repairAttempts?: number | undefined;
  maxRepairAttempts?: number | undefined;
  failedStep?: string | undefined;
  className?: string | undefined;
}

const STATUS_CONFIG: Record<ValidationStatus, {
  icon: React.ReactNode;
  label: string;
  bgColor: string;
  textColor: string;
  borderColor: string;
}> = {
  pending: {
    icon: <Clock className="w-3 h-3" />,
    label: 'Validating...',
    bgColor: 'bg-blue-50 dark:bg-blue-900/20',
    textColor: 'text-blue-600 dark:text-blue-400',
    borderColor: 'border-blue-200 dark:border-blue-800',
  },
  running: {
    icon: <Loader2 className="w-3 h-3 animate-spin" />,
    label: 'Validating...',
    bgColor: 'bg-blue-50 dark:bg-blue-900/20',
    textColor: 'text-blue-600 dark:text-blue-400',
    borderColor: 'border-blue-200 dark:border-blue-800',
  },
  passed: {
    icon: <Check className="w-3 h-3" />,
    label: 'Validated',
    bgColor: 'bg-green-50 dark:bg-green-900/20',
    textColor: 'text-green-600 dark:text-green-400',
    borderColor: 'border-green-200 dark:border-green-800',
  },
  failed: {
    icon: <X className="w-3 h-3" />,
    label: 'Validation failed',
    bgColor: 'bg-red-50 dark:bg-red-900/20',
    textColor: 'text-red-600 dark:text-red-400',
    borderColor: 'border-red-200 dark:border-red-800',
  },
  skipped: {
    icon: <Clock className="w-3 h-3" />,
    label: 'Skipped',
    bgColor: 'bg-gray-50 dark:bg-gray-900/20',
    textColor: 'text-gray-500 dark:text-gray-400',
    borderColor: 'border-gray-200 dark:border-gray-700',
  },
  repairing: {
    icon: <Wrench className="w-3 h-3 animate-pulse" />,
    label: 'Repair in progress',
    bgColor: 'bg-orange-50 dark:bg-orange-900/20',
    textColor: 'text-orange-600 dark:text-orange-400',
    borderColor: 'border-orange-200 dark:border-orange-800',
  },
};

export const ValidationStatusBadge: React.FC<ValidationStatusBadgeProps> = ({
  status,
  repairAttempts,
  maxRepairAttempts = 3,
  failedStep,
  className = '',
}) => {
  const config = STATUS_CONFIG[status];

  if (!config) {
    return null;
  }

  // Check if repair attempts are exhausted
  const isExhausted = repairAttempts !== undefined && repairAttempts >= maxRepairAttempts && status === 'failed';

  // Build the label
  let label = config.label;
  if (status === 'failed' && failedStep) {
    label = `Validation failed (${failedStep})`;
  }
  if (status === 'repairing' && repairAttempts !== undefined) {
    label = `Repair in progress (${repairAttempts}/${maxRepairAttempts})`;
  }
  if (isExhausted) {
    label = 'Repair exhausted - needs attention';
  }

  // Override config for exhausted state
  const displayConfig = isExhausted ? {
    icon: <AlertTriangle className="w-3 h-3" />,
    label,
    bgColor: 'bg-amber-50 dark:bg-amber-900/20',
    textColor: 'text-amber-600 dark:text-amber-400',
    borderColor: 'border-amber-200 dark:border-amber-800',
  } : { ...config, label };

  return (
    <div
      className={`
        inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs font-medium
        border ${displayConfig.bgColor} ${displayConfig.textColor} ${displayConfig.borderColor}
        ${className}
      `}
      title={label}
    >
      {displayConfig.icon}
      <span className="truncate max-w-[150px]">{displayConfig.label}</span>
    </div>
  );
};

export default ValidationStatusBadge;
