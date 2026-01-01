"use client";

import { Progress } from "@/components/ui/progress";
import { Check, X } from "lucide-react";
import { cn } from "@/lib/utils";

interface PasswordStrengthProps {
  password: string;
}

export function PasswordStrength({ password }: PasswordStrengthProps) {
  const requirements = [
    { label: "At least 8 characters", regex: /.{8,}/ },
    { label: "At least one uppercase letter", regex: /[A-Z]/ },
    { label: "At least one lowercase letter", regex: /[a-z]/ },
    { label: "At least one number", regex: /[0-9]/ },
    { label: "At least one special character", regex: /[^A-Za-z0-9]/ },
  ];

  const metRequirements = requirements.filter((req) => req.regex.test(password));
  const score = metRequirements.length;
  const percentage = (score / requirements.length) * 100;

  const getStrengthColor = () => {
    if (score <= 1) return "bg-destructive";
    if (score <= 3) return "bg-yellow-500";
    if (score <= 4) return "bg-blue-500";
    return "bg-green-500";
  };

  const getStrengthLabel = () => {
    if (score === 0) return "";
    if (score <= 1) return "Very Weak";
    if (score <= 3) return "Weak";
    if (score <= 4) return "Good";
    return "Strong";
  };

  if (!password) return null;

  return (
    <div className="space-y-3 pt-2">
      <div className="space-y-1">
        <div className="flex justify-between text-xs">
          <span className="text-muted-foreground font-medium">Password strength</span>
          <span className={cn("font-medium", {
            "text-destructive": score <= 1,
            "text-yellow-500": score > 1 && score <= 3,
            "text-blue-500": score === 4,
            "text-green-500": score === 5,
          })}>
            {getStrengthLabel()}
          </span>
        </div>
        <Progress value={percentage} className="h-1" indicatorClassName={getStrengthColor()} />
      </div>
      <ul className="grid grid-cols-1 gap-1 md:grid-cols-2">
        {requirements.map((req, index) => {
          const isMet = req.regex.test(password);
          return (
            <li key={index} className="flex items-center gap-2 text-xs">
              {isMet ? (
                <Check className="h-3 w-3 text-green-500" />
              ) : (
                <X className="h-3 w-3 text-muted-foreground/50" />
              )}
              <span className={cn(isMet ? "text-foreground" : "text-muted-foreground")}>
                {req.label}
              </span>
            </li>
          );
        })}
      </ul>
    </div>
  );
}