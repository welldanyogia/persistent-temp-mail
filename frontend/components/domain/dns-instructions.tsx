"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Copy, Check, RefreshCw, CheckCircle2, XCircle, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { domainService } from "@/lib/api/domains";
import { DNSStatusResponse } from "@/types/domain";

interface DNSInstructionsProps {
  domainId?: string;
  domainName: string;
  verificationToken?: string;
}

export function DNSInstructions({ domainId, verificationToken }: DNSInstructionsProps) {
  const [copiedMap, setCopiedMap] = useState<Record<string, boolean>>({});
  const [checking, setChecking] = useState(false);
  const [status, setStatus] = useState<DNSStatusResponse | null>(null);

  const copyToClipboard = async (text: string, key: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopiedMap((prev) => ({ ...prev, [key]: true }));
      toast.success("Copied to clipboard");
      setTimeout(() => {
        setCopiedMap((prev) => ({ ...prev, [key]: false }));
      }, 2000);
    } catch (err) {
      toast.error("Failed to copy");
    }
  };

  const checkStatus = async () => {
    if (!domainId) return;
    setChecking(true);
    try {
      const res = await domainService.getDNSStatus(domainId);
      setStatus(res);
      if (res.dns_status.is_ready_to_verify) {
        toast.success("DNS records found! You can now verify.");
      } else {
        toast.error("Some records are missing or incorrect.");
      }
    } catch (err) {
      toast.error("Failed to check status");
    } finally {
      setChecking(false);
    }
  };

  const getRecordStatusIcon = (type: string) => {
    if (!status) return null;
    
    let isValid = false;
    if (type === 'MX') {
      isValid = status.dns_status.mx_records.some((r) => r.is_valid);
    } else if (type === 'TXT') {
      isValid = status.dns_status.txt_records.some((r) => r.is_valid);
    }

    if (isValid) {
      return <CheckCircle2 className="h-5 w-5 text-green-500" />;
    }
    return <XCircle className="h-5 w-5 text-destructive" />;
  };

  const records = [
    {
      type: "MX",
      name: "@",
      value: "mail.webrana.id",
      priority: 10,
      key: "mx",
    },
    {
      type: "TXT",
      name: "_tempmail-verification",
      value: verificationToken || "",
      priority: null,
      key: "txt",
    },
  ];

  return (
    <Card className="w-full border-0 shadow-none sm:border sm:shadow-sm">
      <CardHeader className="px-0 sm:px-6 flex flex-row items-center justify-between space-y-0">
        <div className="space-y-1.5">
          <CardTitle>DNS Configuration</CardTitle>
          <CardDescription>
            Add the following records to your domain&apos;s DNS settings.
          </CardDescription>
        </div>
        {domainId && (
          <Button 
            variant="outline" 
            size="sm" 
            onClick={checkStatus} 
            disabled={checking}
            className="ml-4 shrink-0"
          >
            {checking ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4 mr-2" />}
            Check Status
          </Button>
        )}
      </CardHeader>
      <CardContent className="px-0 sm:px-6">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-[60px] sm:w-[80px]">Type</TableHead>
              <TableHead>Host</TableHead>
              <TableHead>Value</TableHead>
              <TableHead className="hidden sm:table-cell w-[80px]">Priority</TableHead>
              <TableHead className="w-[50px]"></TableHead>
              {status && <TableHead className="w-[50px]">Status</TableHead>}
            </TableRow>
          </TableHeader>
          <TableBody>
            {records.map((record) => (
              <TableRow key={record.key}>
                <TableCell className="font-medium">{record.type}</TableCell>
                <TableCell>{record.name}</TableCell>
                <TableCell className="font-mono text-xs sm:text-sm break-all">
                  {record.value}
                </TableCell>
                <TableCell className="hidden sm:table-cell">{record.priority ?? "-"}</TableCell>
                <TableCell>
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={() => copyToClipboard(record.value, record.key)}
                    title="Copy value"
                  >
                    {copiedMap[record.key] ? (
                      <Check className="h-4 w-4" />
                    ) : (
                      <Copy className="h-4 w-4" />
                    )}
                  </Button>
                </TableCell>
                {status && (
                  <TableCell>
                    <div className="flex justify-center">
                      {getRecordStatusIcon(record.type)}
                    </div>
                  </TableCell>
                )}
              </TableRow>
            ))}
          </TableBody>
        </Table>
        <div className="mt-4 text-xs sm:text-sm text-muted-foreground bg-muted/50 p-3 rounded-md">
          <p>
            <strong>Note:</strong> DNS propagation may take up to 24 hours, though it usually happens within minutes.
            Wait for a few minutes after adding records before verifying.
          </p>
        </div>
      </CardContent>
    </Card>
  );
}